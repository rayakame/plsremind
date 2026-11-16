import type { ReminderLogEntry } from "plsremind";
import { useEffect, useRef, useState } from "react";
import { useNow } from "../../hub/now.js";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Button } from "../../ui/controls.js";
import { Card } from "../../ui/layout.js";
import { formatMoment, reminderLabel } from "./labels.js";

/** What was sent for an item, when and where to. */
export function ReminderLog(props: { itemId: string }) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const now = useNow();
  const [entries, setEntries] = useState<readonly ReminderLogEntry[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [round, setRound] = useState(0);
  const load = useRef(0);
  const paging = useRef<AbortController | undefined>(undefined);
  const { itemId } = props;

  useEffect(() => {
    const abort = new AbortController();
    load.current++;
    setEntries(undefined);
    setCursor(null);
    setLoading(false);
    setFailed(false);
    client.rest.listReminderLog({ itemId, limit: 20 }, { signal: abort.signal }).then(
      (page) => {
        setEntries(page.data);
        setCursor(page.nextCursor);
      },
      () => {
        if (!abort.signal.aborted) setFailed(true);
      },
    );
    return () => {
      abort.abort();
      paging.current?.abort();
    };
  }, [client, itemId, round]);

  const more = async () => {
    if (cursor === null || loading) return;
    const mine = load.current;
    const abort = (paging.current = new AbortController());
    setLoading(true);
    try {
      const page = await client.rest.listReminderLog(
        { itemId, limit: 20, cursor },
        { signal: abort.signal },
      );
      if (load.current !== mine) return;
      setEntries((before) => [...(before ?? []), ...page.data]);
      setCursor(page.nextCursor);
    } catch (error) {
      if (load.current !== mine || abort.signal.aborted) return;
      void guard(Promise.reject(error));
    }
    setLoading(false);
  };

  const result = (entry: ReminderLogEntry) =>
    entry.deliveries.length > 0
      ? entry.deliveries
          .map((delivery) => `${delivery.channel.name}: ${t(`status.${delivery.status}`)}`)
          .join(", ")
      : t(`outcome.${entry.outcome}`);

  return (
    <Card title={t("item.sent")}>
      {failed && (
        <div className="between">
          <span className="dim">{t("errors.network")}</span>
          <Button onClick={() => setRound((before) => before + 1)}>{t("common.retry")}</Button>
        </div>
      )}
      {entries?.length === 0 && <p className="text dim">{t("item.sentNone")}</p>}
      {entries && entries.length > 0 && (
        <div role="list" className="stack">
          {entries.map((entry) => (
            <div role="listitem" className="kv" key={entry.id}>
              <span>{reminderLabel(entry.kind, entry.offset)}</span>
              <span>{formatMoment(entry.firedAt, now)}</span>
              <span>{result(entry)}</span>
            </div>
          ))}
        </div>
      )}
      {cursor !== null && (
        <Button disabled={loading} onClick={() => void more()}>
          {t("item.loadMore")}
        </Button>
      )}
    </Card>
  );
}
