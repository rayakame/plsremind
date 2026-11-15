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
  const shown = useRef(props.itemId);
  const { itemId } = props;

  useEffect(() => {
    const abort = new AbortController();
    shown.current = itemId;
    setEntries(undefined);
    setCursor(null);
    setLoading(false);
    client.rest.listReminderLog({ itemId, limit: 20 }, { signal: abort.signal }).then(
      (page) => {
        setEntries(page.data);
        setCursor(page.nextCursor);
      },
      () => {},
    );
    return () => abort.abort();
  }, [client, itemId]);

  const more = async () => {
    if (cursor === null || loading) return;
    setLoading(true);
    const page = await guard(client.rest.listReminderLog({ itemId, limit: 20, cursor }));
    if (shown.current !== itemId) return;
    setLoading(false);
    if (!page) return;
    setEntries((before) => [...(before ?? []), ...page.data]);
    setCursor(page.nextCursor);
  };

  const result = (entry: ReminderLogEntry) =>
    entry.deliveries.length > 0
      ? entry.deliveries
          .map((delivery) => `${delivery.channel.name}: ${t(`status.${delivery.status}`)}`)
          .join(", ")
      : t(`outcome.${entry.outcome}`);

  return (
    <Card title={t("item.sent")}>
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
