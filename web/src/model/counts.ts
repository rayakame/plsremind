import type { Item } from "plsremind";

const cache = new WeakMap<readonly Item[], ReadonlyMap<string, number>>();

/** How many open items each list holds, by list id. */
export function openCounts(items: readonly Item[]): ReadonlyMap<string, number> {
  let counts = cache.get(items);
  if (!counts) {
    const fresh = new Map<string, number>();
    for (const item of items) fresh.set(item.listId, (fresh.get(item.listId) ?? 0) + 1);
    cache.set(items, (counts = fresh));
  }
  return counts;
}
