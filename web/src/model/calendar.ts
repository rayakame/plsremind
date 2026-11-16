import type { Item } from "plsremind";
import { dayIn } from "../time/zone.js";

interface Memo {
  done: readonly Item[];
  zone: string;
  result: ReadonlyMap<string, readonly Item[]>;
}

const memo = new WeakMap<readonly Item[], Memo>();
const time = (item: Item) => (item.allDay ? -1 : (item.dueAt?.getTime() ?? 0));

/** Items with a due time by their day in the zone: whole-day ones first, then by time. */
export function byDay(
  open: readonly Item[],
  done: readonly Item[],
  zone: string,
): ReadonlyMap<string, readonly Item[]> {
  const before = memo.get(open);
  if (before && before.done === done && before.zone === zone) return before.result;
  const days = new Map<string, Item[]>();
  for (const items of [open, done]) {
    for (const item of items) {
      const day = item.allDay ? item.dueDate : item.dueAt ? dayIn(item.dueAt, zone) : null;
      if (!day) continue;
      const own = days.get(day);
      if (own) own.push(item);
      else days.set(day, [item]);
    }
  }
  for (const items of days.values()) {
    items.sort((a, b) => time(a) - time(b) || a.createdAt.getTime() - b.createdAt.getTime());
  }
  memo.set(open, { done, zone, result: days });
  return days;
}
