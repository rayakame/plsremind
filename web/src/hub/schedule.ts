import type { Item, ScheduledReminder } from "plsremind";
import { useEffect, useState } from "react";
import { usePlans, useProfile } from "./hooks.js";
import { useHub } from "./session.js";

// Long enough for a change shown ahead to have reached the hub before the schedule is asked for.
const settle = 400;

/** The parts of an item that decide when the hub reminds about it. */
function scheduleKey(item: Item): string {
  return JSON.stringify([
    item.status,
    item.dueAt?.getTime() ?? null,
    item.dueDate,
    item.priority,
    item.reminderOverride,
    item.reminders.map((reminder) => [reminder.id, reminder.remindAt.getTime()]),
  ]);
}

/**
 * What the hub is going to send for an item. Undefined until the first answer; afterwards
 * the last answer stays while a newer one loads.
 */
export function useSchedule(item: Item | undefined): readonly ScheduledReminder[] | undefined {
  const { client } = useHub();
  const plans = usePlans();
  const profile = useProfile();
  const [loaded, setLoaded] = useState<{ id: string; entries: readonly ScheduledReminder[] }>();
  const id = item?.id;
  const open = item?.status === "open" && !id?.startsWith("tmp_");
  const key = item ? scheduleKey(item) : "";
  const quiet = profile.quietHours ? `${profile.quietHours.start}-${profile.quietHours.end}` : "";
  const first = loaded?.id !== id;

  useEffect(() => {
    if (!id || !open) return;
    const abort = new AbortController();
    const timer = setTimeout(
      () =>
        client.rest.getItemSchedule(id, { signal: abort.signal }).then(
          (entries) => setLoaded({ id, entries }),
          () => {},
        ),
      first ? 0 : settle,
    );
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
    // "first" only picks the delay; it must not start another request once the answer is in.
  }, [client, id, open, key, plans, quiet, profile.timezone]);

  if (!id || !open) return [];
  return loaded?.id === id ? loaded.entries : undefined;
}
