import { ApiError, type Item } from "plsremind";
import { useEffect, useState } from "react";
import { useLocation } from "wouter";
import { useItem, useResolved } from "../hub/hooks.js";
import { useHub } from "../hub/session.js";
import { removeItem, setDone } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { useNotices } from "../shell/notices.js";
import { routes } from "../shell/router.js";
import { useLeaveItem, useSelection } from "../shell/selection.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { ConfirmDialog } from "../ui/name-dialog.js";
import { ItemFacts, ItemText } from "./item/fields.js";
import { ReminderLog } from "./item/log.js";
import { NotFound } from "./not-found.js";

type Fetched = { item: Item | null } | { failed: true };

function useFetched(id: string, skip: boolean): { state: Fetched | undefined; retry: () => void } {
  const { client } = useHub();
  const { guard } = useNotices();
  const [fetched, setFetched] = useState<{ id: string; state: Fetched }>();
  const [round, setRound] = useState(0);
  useEffect(() => {
    if (skip) return;
    const abort = new AbortController();
    client.rest.getItem(id, { signal: abort.signal }).then(
      (item) => setFetched({ id, state: { item } }),
      (error: unknown) => {
        if (abort.signal.aborted) return;
        if (error instanceof ApiError && error.status === 404) {
          return setFetched({ id, state: { item: null } });
        }
        setFetched({ id, state: { failed: true } });
        void guard(Promise.reject(error));
      },
    );
    return () => abort.abort();
  }, [client, id, skip, guard, round]);
  return {
    state: fetched?.id === id ? fetched.state : undefined,
    retry: () => {
      setFetched(undefined);
      setRound((before) => before + 1);
    },
  };
}

export function ItemPage(props: { id: string }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const { order, setPageItem } = useSelection();
  const leave = useLeaveItem();
  const [, navigate] = useLocation();
  const { id, key } = useResolved(props.id);
  const shown = id ?? props.id;
  const live = useItem(shown);
  const [seen, setSeen] = useState<string>();
  const [deleting, setDeleting] = useState(false);
  useEffect(() => {
    if (live) setSeen(live.id);
  }, [live]);
  useEffect(() => {
    if (shown !== props.id) navigate(routes.item(shown), { replace: true });
  }, [shown, props.id, navigate]);
  const draft = shown.startsWith("tmp_");
  const gone = !live && (seen === shown || draft);
  const fetched = useFetched(shown, live !== undefined || gone);

  if (!live && !gone && fetched.state && "failed" in fetched.state) {
    return (
      <>
        <h1 className="h1">{t("item.loadFailed")}</h1>
        <div className="line">
          <Button onClick={fetched.retry}>{t("common.retry")}</Button>
        </div>
      </>
    );
  }
  const loaded = fetched.state && "item" in fetched.state ? fetched.state.item : undefined;
  const item = live ?? (gone ? null : loaded);
  const fetchedItem = live ? undefined : (item ?? undefined);
  useEffect(() => {
    setPageItem(fetchedItem);
    return () => setPageItem(undefined);
  }, [fetchedItem, setPageItem]);
  if (item === undefined) return null;
  if (item === null) return <NotFound />;

  const at = order.indexOf(item.id);
  const go = (by: number) => {
    const next = order[at + by];
    if (next !== undefined) navigate(routes.item(next), { replace: true, state: history.state });
  };
  const done = item.status === "done";

  return (
    <div className="scroll col">
      <div className="between">
        <div className="line">
          <Button variant="ghost" onClick={leave}>
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
        <ItemText key={`text:${key}`} item={item} readOnly={!live} />
        <div className="col">
          <ItemFacts key={`facts:${key}`} item={item} readOnly={!live} />
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
