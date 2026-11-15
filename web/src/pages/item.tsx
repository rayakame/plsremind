import { ApiError, type Item } from "plsremind";
import { useEffect, useState } from "react";
import { Redirect, useLocation } from "wouter";
import { useItem } from "../hub/hooks.js";
import { useHub } from "../hub/session.js";
import { removeItem, setDone } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { useNotices } from "../shell/notices.js";
import { routes } from "../shell/router.js";
import { useSelection } from "../shell/selection.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { ConfirmDialog } from "../ui/name-dialog.js";
import { ItemFacts, ItemText } from "./item/fields.js";
import { ReminderLog } from "./item/log.js";
import { NotFound } from "./not-found.js";

/** An item the cache does not hold, such as one completed long ago. */
function useFetched(id: string, skip: boolean): Item | null | undefined {
  const { client } = useHub();
  const { guard } = useNotices();
  const [fetched, setFetched] = useState<{ id: string; item: Item | null }>();
  useEffect(() => {
    if (skip) return;
    const abort = new AbortController();
    client.rest.getItem(id, { signal: abort.signal }).then(
      (item) => setFetched({ id, item }),
      (error: unknown) => {
        if (abort.signal.aborted) return;
        if (error instanceof ApiError && error.status === 404) setFetched({ id, item: null });
        else void guard(Promise.reject(error));
      },
    );
    return () => abort.abort();
  }, [client, id, skip, guard]);
  return fetched?.id === id ? fetched.item : undefined;
}

export function ItemPage(props: { id: string }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const { order } = useSelection();
  const [, navigate] = useLocation();
  const live = useItem(props.id);
  const [seen, setSeen] = useState<string>();
  const [deleting, setDeleting] = useState(false);
  useEffect(() => {
    if (live) setSeen(live.id);
  }, [live]);
  const gone = !live && seen === props.id;
  const fetched = useFetched(props.id, live !== undefined || gone || props.id.startsWith("tmp_"));

  const real = props.id.startsWith("tmp_") ? hub.view.resolveId(props.id) : undefined;
  if (!live && real) return <Redirect to={routes.item(real)} replace />;
  const item = live ?? (gone ? null : fetched);
  if (item === undefined) return null;
  if (item === null) return <NotFound />;

  const at = order.indexOf(item.id);
  const go = (by: number) => {
    const next = order[at + by];
    if (next !== undefined) navigate(routes.item(next), { replace: true });
  };
  const done = item.status === "done";

  return (
    <div className="scroll col">
      <div className="between">
        <div className="line">
          <Button variant="ghost" onClick={() => history.back()}>
            <Icon name="left" />
            {t("item.back")}
          </Button>
          {at >= 0 && (
            <>
              <Button disabled={at === 0} onClick={() => go(-1)}>
                {t("item.previous")}
              </Button>
              <Button disabled={at === order.length - 1} onClick={() => go(1)}>
                {t("item.next")}
              </Button>
            </>
          )}
        </div>
        <div className="line">
          <Button
            variant={done ? undefined : "primary"}
            onClick={() => void guard(setDone(hub, item, !done))}
          >
            {t(done ? "item.reopen" : "item.complete")}
          </Button>
          <Button variant="icon" aria-label={t("item.delete")} onClick={() => setDeleting(true)}>
            <Icon name="trash" />
          </Button>
        </div>
      </div>
      <div className="split">
        <ItemText key={`text:${item.id}`} item={item} readOnly={!live} />
        <div className="col">
          <ItemFacts key={`facts:${item.id}`} item={item} readOnly={!live} />
          <ReminderLog itemId={item.id} />
        </div>
      </div>
      {deleting && (
        <ConfirmDialog
          title={t("item.delete")}
          text={t("item.deleteText")}
          confirm={t("common.delete")}
          onConfirm={() => {
            void guard(removeItem(hub, item));
            navigate(routes.items);
          }}
          onClose={() => setDeleting(false)}
        />
      )}
    </div>
  );
}
