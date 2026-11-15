import type { Item } from "plsremind";
import { addDays, dayIn, instantAt, weekStartOf } from "../time/zone.js";

export type GroupKey = "overdue" | "today" | "week" | "later" | "none";

export interface Group {
  readonly key: GroupKey;
  readonly items: readonly Item[];
}

export type DoneKey = "today" | "yesterday" | "week" | "month" | "older";

export interface DoneGroup {
  readonly key: DoneKey;
  readonly items: readonly Item[];
}

/** The present moment as the views need it, in the user's zone. */
export interface Now {
  readonly instant: Date;
  readonly today: string;
  /** The last day of the current week. */
  readonly weekEnd: string;
  readonly weekStart: string;
  readonly zone: string;
}

export function nowIn(zone: string, weekStart: "monday" | "sunday", instant = new Date()): Now {
  const today = dayIn(instant, zone);
  const start = weekStartOf(today, weekStart);
  return { instant, today, weekStart: start, weekEnd: addDays(start, 6), zone };
}

const order: readonly GroupKey[] = ["overdue", "today", "week", "later", "none"];
const rank = { high: 0, normal: 1, low: 2 } as const;
const due = (item: Item) => item.dueAt?.getTime() ?? Number.POSITIVE_INFINITY;
const byDue = (a: Item, b: Item) =>
  due(a) - due(b) || a.createdAt.getTime() - b.createdAt.getTime();
const byPriority = (a: Item, b: Item) => rank[a.priority] - rank[b.priority] || byDue(a, b);

interface Memo<T> {
  key: string;
  result: T;
}

const openMemo = new WeakMap<readonly Item[], Memo<readonly Group[]>>();
const doneMemo = new WeakMap<readonly Item[], Memo<readonly DoneGroup[]>>();

/** Open items by how soon they are due. Empty groups are left out. */
export function groupOpen(
  items: readonly Item[],
  now: Now,
  sort: "due" | "priority",
): readonly Group[] {
  const minute = Math.floor(now.instant.getTime() / 60_000);
  const key = `${now.zone}|${now.today}|${now.weekEnd}|${minute}|${sort}`;
  const memo = openMemo.get(items);
  if (memo?.key === key) return memo.result;

  const present = now.instant.getTime();
  const tomorrow = instantAt(addDays(now.today, 1), "00:00", now.zone).getTime();
  const nextWeek = instantAt(addDays(now.weekEnd, 1), "00:00", now.zone).getTime();
  const buckets: Record<GroupKey, Item[]> = {
    overdue: [],
    today: [],
    week: [],
    later: [],
    none: [],
  };
  for (const item of items) {
    let group: GroupKey;
    if (item.allDay && item.dueDate) {
      if (item.dueDate < now.today) group = "overdue";
      else if (item.dueDate === now.today) group = "today";
      else group = item.dueDate <= now.weekEnd ? "week" : "later";
    } else if (!item.dueAt) {
      group = "none";
    } else {
      const at = item.dueAt.getTime();
      if (at < present) group = "overdue";
      else if (at < tomorrow) group = "today";
      else group = at < nextWeek ? "week" : "later";
    }
    buckets[group].push(item);
  }
  const compare = sort === "due" ? byDue : byPriority;
  const result = Object.freeze(
    order
      .filter((group) => buckets[group].length > 0)
      .map((group) => ({ key: group, items: Object.freeze(buckets[group].sort(compare)) })),
  );
  openMemo.set(items, { key, result });
  return result;
}

const doneOrder: readonly DoneKey[] = ["today", "yesterday", "week", "month", "older"];

/** Completed items, given newest first, by how long ago they were completed. */
export function doneGroups(items: readonly Item[], now: Now): readonly DoneGroup[] {
  const key = `${now.zone}|${now.today}|${now.weekStart}`;
  const memo = doneMemo.get(items);
  if (memo?.key === key) return memo.result;

  const start = (day: string) => instantAt(day, "00:00", now.zone).getTime();
  const today = start(now.today);
  const yesterday = start(addDays(now.today, -1));
  const week = start(now.weekStart);
  const month = start(`${now.today.slice(0, 7)}-01`);
  const buckets: Record<DoneKey, Item[]> = {
    today: [],
    yesterday: [],
    week: [],
    month: [],
    older: [],
  };
  for (const item of items) {
    const at = item.completedAt?.getTime() ?? 0;
    const group: DoneKey =
      at >= today
        ? "today"
        : at >= yesterday
          ? "yesterday"
          : at >= week
            ? "week"
            : at >= month
              ? "month"
              : "older";
    buckets[group].push(item);
  }
  const result = Object.freeze(
    doneOrder
      .filter((group) => buckets[group].length > 0)
      .map((group) => ({ key: group, items: Object.freeze(buckets[group]) })),
  );
  doneMemo.set(items, { key, result });
  return result;
}

/** The ids in the order the groups show them. */
export function flatOrder(groups: readonly { items: readonly Item[] }[]): readonly string[] {
  return groups.flatMap((group) => group.items.map((item) => item.id));
}
