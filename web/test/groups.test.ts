import { expect, test } from "vitest";
import { doneGroups, flatOrder, groupOpen, nowIn } from "../src/model/groups.js";
import { filterItems } from "../src/model/search.js";
import { allDay, item, timed } from "./items.js";

const zone = "Europe/Berlin";
// Sunday 15 November 2026, 23:59 in Berlin.
const late = nowIn(zone, "monday", new Date("2026-11-15T22:59:00Z"));
const ids = (items: readonly { id: string }[]) => items.map((one) => one.id);
const shape = (groups: readonly { key: string; items: readonly { id: string }[] }[]) =>
  groups.map((group) => `${group.key}: ${ids(group.items).join(" ")}`);

test("now knows today and the end of the week in the user's zone", () => {
  expect(late.today).toBe("2026-11-15");
  expect(late.weekEnd).toBe("2026-11-15");
  expect(nowIn(zone, "sunday", new Date("2026-11-15T22:59:00Z")).weekEnd).toBe("2026-11-21");
  expect(nowIn(zone, "monday", new Date("2026-11-15T23:00:00Z")).today).toBe("2026-11-16");
});

test("open items fall into overdue, today, this week, later and no date", () => {
  const now = nowIn(zone, "monday", new Date("2026-11-11T11:00:00Z"));
  const items = [
    item({ id: "none" }),
    timed("later", "2026-11-16T08:00:00Z"),
    allDay("sunday", "2026-11-15"),
    timed("tonight", "2026-11-11T20:00:00Z"),
    timed("just-missed", "2026-11-11T10:59:00Z"),
    allDay("today", "2026-11-11"),
    allDay("yesterday", "2026-11-10"),
    timed("thursday", "2026-11-12T08:00:00Z"),
  ];
  expect(shape(groupOpen(items, now, "due"))).toEqual([
    "overdue: yesterday just-missed",
    "today: tonight today",
    "week: thursday sunday",
    "later: later",
    "none: none",
  ]);
});

test("an all-day item of today is not overdue one minute before midnight", () => {
  expect(shape(groupOpen([allDay("a", "2026-11-15")], late, "due"))).toEqual(["today: a"]);
});

test("a time just after midnight in the user's zone belongs to tomorrow", () => {
  const now = nowIn(zone, "monday", new Date("2026-11-10T11:00:00Z"));
  expect(shape(groupOpen([timed("a", "2026-11-10T23:30:00Z")], now, "due"))).toEqual(["week: a"]);
});

test("within a group the order follows the due time or the priority", () => {
  const now = nowIn(zone, "monday", new Date("2026-11-09T05:00:00Z"));
  const items = [
    timed("c", "2026-11-09T18:00:00Z", { priority: "low" }),
    timed("a", "2026-11-09T09:00:00Z"),
    timed("b", "2026-11-09T12:00:00Z", { priority: "high" }),
    item({ id: "new", createdAt: new Date("2026-11-05T00:00:00Z") }),
    item({ id: "old", createdAt: new Date("2026-11-02T00:00:00Z"), priority: "high" }),
  ];
  expect(shape(groupOpen(items, now, "due"))).toEqual(["today: a b c", "none: old new"]);
  expect(shape(groupOpen(items, now, "priority"))).toEqual(["today: b a c", "none: old new"]);
  expect(flatOrder(groupOpen(items, now, "due"))).toEqual(["a", "b", "c", "old", "new"]);
});

test("the same input gives the very same result", () => {
  const items = [item({ id: "a" })];
  const now = nowIn(zone, "monday", new Date("2026-11-09T05:00:00Z"));
  expect(groupOpen(items, now, "due")).toBe(groupOpen(items, now, "due"));
  expect(groupOpen(items, now, "priority")).not.toBe(groupOpen(items, now, "due"));
});

test("done items are grouped by how long ago they were completed", () => {
  const now = nowIn(zone, "monday", new Date("2026-11-11T11:00:00Z"));
  const done = (id: string, at: string) => item({ id, status: "done", completedAt: new Date(at) });
  const items = [
    done("now", "2026-11-11T10:00:00Z"),
    done("midnight", "2026-11-10T23:00:00Z"),
    done("yesterday", "2026-11-10T22:59:00Z"),
    done("monday", "2026-11-09T08:00:00Z"),
    done("month", "2026-11-02T08:00:00Z"),
    done("older", "2026-10-31T22:59:00Z"),
  ];
  expect(shape(doneGroups(items, now))).toEqual([
    "today: now midnight",
    "yesterday: yesterday",
    "week: monday",
    "month: month",
    "older: older",
  ]);
});

test("a search folds case and accents and wants every word", () => {
  const items = [
    item({ id: "a", title: "Über den Report reden" }),
    item({ id: "b", title: "Report", note: "for the UBER meeting" }),
    item({ id: "c", title: "Reply to Uber" }),
  ];
  expect(ids(filterItems(items, "uber rep"))).toEqual(["a", "b", "c"]);
  expect(ids(filterItems(items, "  report   uber "))).toEqual(["a", "b"]);
  expect(ids(filterItems(items, "ÜBER reden"))).toEqual(["a"]);
  expect(filterItems(items, "")).toBe(items);
  expect(filterItems(items, "   ")).toBe(items);
  expect(filterItems(items, "rep")).toBe(filterItems(items, "rep"));
  expect(filterItems(items, "zzz")).toEqual([]);
});

test("an item whose time passes within the same minute becomes overdue", () => {
  const items = [timed("a", "2026-11-09T12:00:30Z")];
  const before = nowIn(zone, "monday", new Date("2026-11-09T12:00:10Z"));
  const after = nowIn(zone, "monday", new Date("2026-11-09T12:00:40Z"));
  expect(shape(groupOpen(items, before, "due"))).toEqual(["today: a"]);
  expect(shape(groupOpen(items, after, "due"))).toEqual(["overdue: a"]);
});
