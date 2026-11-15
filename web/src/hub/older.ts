import type { Item } from "plsremind";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useHistory, useItems, useProfile } from "./hooks.js";
import { useHub } from "./session.js";

interface Loaded {
  items: readonly Item[];
  /** Where the next page starts; undefined before the first page, null after the last. */
  cursor: string | null | undefined;
}

const nothing: Loaded = { items: [], cursor: undefined };

/**
 * Completed items older than what the cache holds, loaded on demand. They start over when
 * the cache was filled anew or the time zone changed, since both move the hub's order.
 */
export function useOlderDone() {
  const { client, view, userId } = useHub();
  const profile = useProfile();
  const history = useHistory();
  const open = useItems();
  const start = view.user(userId)?.historyCursor ?? null;
  const [loaded, setLoaded] = useState(nothing);
  const [loading, setLoading] = useState(false);
  const epoch = useRef(0);

  useEffect(() => {
    epoch.current++;
    setLoaded(nothing);
    setLoading(false);
  }, [client, start, profile.timezone]);

  const cursor = loaded.cursor === undefined ? start : loaded.cursor;
  const loadMore = useCallback(async () => {
    if (cursor === null || loading) return;
    const mine = epoch.current;
    setLoading(true);
    try {
      const page = await client.rest.listItems({
        status: "done",
        sort: "completed",
        order: "desc",
        limit: 100,
        cursor,
      });
      if (mine !== epoch.current) return;
      setLoaded((before) => ({ items: [...before.items, ...page.data], cursor: page.nextCursor }));
    } finally {
      if (mine === epoch.current) setLoading(false);
    }
  }, [client, cursor, loading]);

  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const items = useMemo(() => {
    const known = new Set([...history, ...open].map((item) => item.id));
    return loaded.items.filter((item) => !known.has(item.id) && !hidden.has(item.id));
  }, [loaded.items, history, open, hidden]);

  const hide = useCallback((id: string, on: boolean) => {
    setHidden((before) => {
      const next = new Set(before);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  return { items, more: cursor !== null, loading, loadMore, hide };
}
