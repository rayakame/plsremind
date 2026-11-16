import type { Item } from "plsremind";

/** An item as the SDK hands it out, for tests of pure functions. */
export function item(overrides: Partial<Item> & { id: string }): Item {
  return {
    listId: "l1",
    title: `Item ${overrides.id}`,
    note: "",
    priority: "normal",
    dueAt: null,
    dueDate: null,
    allDay: false,
    status: "open",
    completedAt: null,
    reminderOverride: null,
    subitems: [],
    reminders: [],
    createdAt: new Date("2026-11-01T10:00:00Z"),
    updatedAt: new Date("2026-11-01T10:00:00Z"),
    ...overrides,
  };
}

export function timed(id: string, at: string, more: Partial<Item> = {}): Item {
  return item({ id, dueAt: new Date(at), ...more });
}

/** An all-day item; the hub puts its instant at the end of that day in the user's zone. */
export function allDay(id: string, day: string, more: Partial<Item> = {}): Item {
  return item({
    id,
    dueDate: day,
    allDay: true,
    dueAt: new Date(new Date(`${day}T22:59:59Z`).getTime()),
    ...more,
  });
}

export function many(count: number): Item[] {
  const start = Date.parse("2026-11-01T00:00:00Z");
  return Array.from({ length: count }, (_, i) =>
    item({
      id: `i${i}`,
      listId: `l${i % 7}`,
      title: `Task number ${i} ${i % 3 === 0 ? "report" : "call"}`,
      note: i % 5 === 0 ? "Über den Bericht reden" : "",
      priority: i % 11 === 0 ? "high" : i % 4 === 0 ? "low" : "normal",
      dueAt: i % 6 === 0 ? null : new Date(start + ((i * 7919) % (60 * 86_400_000))),
      createdAt: new Date(start - i * 60_000),
    }),
  );
}
