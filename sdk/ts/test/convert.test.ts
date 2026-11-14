import { expect, test } from "vitest";
import { fromWire, microsOf, toWire } from "../src/convert.js";
import type { Delivery, Item, ReminderPlan, User } from "../src/index.js";
import { wireDelivery, wireItem, wirePlan, wireUser } from "./fixtures.js";

test("an item arrives with idiomatic names and real types", () => {
  const item = fromWire<Item>(
    wireItem({
      due_at: "2026-11-13T18:00:00Z",
      list_id: "l1",
      reminder_override: { mode: "custom", before: ["1d", "3h"] },
      subitems: [
        {
          id: "s1",
          item_id: "i1",
          title: "a",
          done: false,
          position: 0,
          created_at: "2026-11-01T10:00:00.123456Z",
          updated_at: "2026-11-01T10:00:00Z",
        },
      ],
    }),
  );
  expect(item.dueAt).toEqual(new Date("2026-11-13T18:00:00Z"));
  expect(item.completedAt).toBeNull();
  expect(item.listId).toBe("l1");
  expect(item.reminderOverride).toEqual({ mode: "custom", before: [86_400_000, 10_800_000] });
  expect(item.subitems[0]!.itemId).toBe("i1");
  expect(item.subitems[0]!.createdAt).toBeInstanceOf(Date);
  expect("due_at" in item).toBe(false);
});

test("an all-day date and quiet hours stay text", () => {
  const item = fromWire<Item>(wireItem({ due_date: "2026-11-13", all_day: true }));
  expect(item.dueDate).toBe("2026-11-13");
  expect(item.allDay).toBe(true);
  const user = fromWire<User>(wireUser({ quiet_hours: { start: "22:00", end: "07:00" } }));
  expect(user.quietHours).toEqual({ start: "22:00", end: "07:00" });
  expect(user.displayName).toBe("Mia");
});

test("a plan and a delivery carry durations in milliseconds", () => {
  expect(fromWire<ReminderPlan>(wirePlan({ before: ["1d", "90m"] }))).toEqual({
    priority: "normal",
    before: [86_400_000, 5_400_000],
    followUp: "never",
    isDefault: true,
  });
  const delivery = fromWire<Delivery>(wireDelivery());
  expect(delivery.reminder.offset).toBe(86_400_000);
  expect(delivery.reminder.kind).toBe("before");
  expect(delivery.reminder.scheduledFor).toEqual(new Date("2026-11-12T18:00:00Z"));
  expect(delivery.user.externalIds).toEqual(["1234"]);
  expect(fromWire<{ offset: null }>({ offset: null }).offset).toBeNull();
});

test("preferences pass through untouched in both directions", () => {
  const preferences = { sort_by: { due_at: "2026-01-01T00:00:00Z" }, before: ["x"] };
  expect(fromWire<User>(wireUser({ preferences })).preferences).toBe(preferences);
  expect(toWire({ displayName: "Mia", preferences })).toEqual({ display_name: "Mia", preferences });
});

test("a field the SDK does not know keeps its value under a camelCase name", () => {
  expect(fromWire({ id: "x", brand_new: { inner_key: "2026-01-01T00:00:00Z" } })).toEqual({
    id: "x",
    brandNew: { innerKey: "2026-01-01T00:00:00Z" },
  });
});

test("a request body goes out in the hub's form", () => {
  expect(
    toWire({
      dueAt: new Date("2026-11-13T18:00:00Z"),
      listId: "l1",
      note: undefined,
      reminderOverride: { mode: "custom", before: [86_400_000] },
    }),
  ).toEqual({
    due_at: "2026-11-13T18:00:00.000Z",
    list_id: "l1",
    reminder_override: { mode: "custom", before: ["1d"] },
  });
  expect(toWire({ in: 60_000 })).toEqual({ in: "1m" });
  expect(toWire({ expiresIn: 86_400_000 })).toEqual({ expires_in: "1d" });
  expect(toWire({ dueAt: null, ids: ["a", "b"] })).toEqual({ due_at: null, ids: ["a", "b"] });
});

test("microsOf keeps what a Date drops", () => {
  expect(microsOf("2026-11-13T18:00:00.123456Z") - microsOf("2026-11-13T18:00:00.123455Z")).toBe(
    1n,
  );
  expect(microsOf("2026-11-13T18:00:00Z")).toBe(BigInt(Date.parse("2026-11-13T18:00:00Z")) * 1000n);
  expect(microsOf("2026-11-13T19:00:00.5+01:00")).toBe(microsOf("2026-11-13T18:00:00.500Z"));
  expect(microsOf("2026-11-13T18:00:00.1234567Z")).toBe(microsOf("2026-11-13T18:00:00.123456Z"));
});

test("converting a sync of 5000 items stays quick", () => {
  const items = Array.from({ length: 5000 }, (_, i) =>
    wireItem({ id: `i${i}`, due_at: "2026-11-13T18:00:00Z" }),
  );
  fromWire({ items: items.slice(0, 50) });
  const started = performance.now();
  fromWire({ items });
  expect(performance.now() - started).toBeLessThan(250);
});
