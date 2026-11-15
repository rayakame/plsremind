import type { Item, ScheduledReminder } from "plsremind";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { usePlans, useProfile } from "./hooks.js";
import { useHub } from "./session.js";
import { answeredCalls, onAnsweredCall } from "./write.js";

function remindersKey(item: Item): string {
  return item.reminders
    .filter((reminder) => !reminder.id.startsWith("tmp_"))
    .map((reminder) => `${reminder.id}@${reminder.remindAt.getTime()}`)
    .join();
}

/**
 * What the hub is going to send for an item. Undefined until the first answer; afterwards
 * the last answer stays while a newer one loads.
 */
export function useSchedule(item: Item | undefined): readonly ScheduledReminder[] | undefined {
  const hub = useHub();
  const { client } = hub;
  const plans = usePlans();
  const profile = useProfile();
  const [loaded, setLoaded] = useState<{ id: string; entries: readonly ScheduledReminder[] }>();
  const id = item?.id;
  const open = item?.status === "open" && !id?.startsWith("tmp_");
  // Asked again when the hub changed the item, when a reminder came or went, and when a
  // call of our own came back. A change that is only shown ahead would get the old answer.
  const confirmed = item?.updatedAt.getTime();
  const reminders = item ? remindersKey(item) : "";
  const subscribe = useCallback((notify: () => void) => onAnsweredCall(hub, notify), [hub]);
  const answered = useSyncExternalStore(subscribe, () => (id ? answeredCalls(hub, id) : 0));
  const quiet = profile.quietHours ? `${profile.quietHours.start}-${profile.quietHours.end}` : "";

  useEffect(() => {
    if (!id || !open) return;
    const abort = new AbortController();
    client.rest.getItemSchedule(id, { signal: abort.signal }).then(
      (entries) => setLoaded({ id, entries }),
      () => {},
    );
    return () => abort.abort();
  }, [client, id, open, confirmed, reminders, answered, plans, quiet, profile.timezone]);

  if (!id || !open) return [];
  return loaded?.id === id ? loaded.entries : undefined;
}
