import type { Item, ScheduledReminder } from "plsremind";
import { useEffect, useState } from "react";
import { usePlans, useProfile } from "./hooks.js";
import { useHub } from "./session.js";

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
  const quiet = profile.quietHours ? `${profile.quietHours.start}-${profile.quietHours.end}` : "";

  useEffect(() => {
    if (!id || !open) return;
    const abort = new AbortController();
    client.rest.getItemSchedule(id, { signal: abort.signal }).then(
      (entries) => setLoaded({ id, entries }),
      () => {},
    );
    return () => abort.abort();
  }, [client, id, open, item, plans, quiet, profile.timezone]);

  if (!id || !open) return [];
  return loaded?.id === id ? loaded.entries : undefined;
}
