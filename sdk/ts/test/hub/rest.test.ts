import { expect, test } from "vitest";
import { ApiError } from "../../src/index.js";
import { admin, hub, session } from "./helpers.js";

test("the hub is up and tells where its gateway is", async () => {
  const { rest } = await session("probe");
  const info = await rest.getInfo();
  expect(info.setupRequired).toBe(false);
  expect(info.gatewayUrl).toBe(`${hub.url.replace("http", "ws")}/api/v1/gateway`);
  expect(info.loginMethods.map((method) => method.type)).toContain("password");
});

test("validation errors name fields the way the SDK does", async () => {
  const { rest } = await session("mia");
  const error = await rest
    .createItem({ title: "both", dueAt: new Date("2030-01-01T10:00:00Z"), dueDate: "2030-01-01" })
    .catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  const fields = (error as ApiError).fields!;
  expect((error as ApiError).status).toBe(422);
  expect(Object.keys(fields).length).toBeGreaterThan(0);
  for (const name of Object.keys(fields)) expect(name).not.toContain("_");
  expect(Object.keys(fields).some((name) => name === "dueAt" || name === "dueDate")).toBe(true);
});

test("an item round-trips with dates, durations and an all-day date", async () => {
  const { rest } = await session("mia");
  const dueAt = new Date("2030-03-01T09:30:00.000Z");
  const timed = await rest.createItem({
    title: "timed",
    note: "bring the slides",
    priority: "high",
    dueAt,
    reminderOverride: { mode: "custom", before: [129_600_000, 5_400_000] },
  });
  expect(timed.dueAt).toEqual(dueAt);
  expect(timed.allDay).toBe(false);
  expect(timed.reminderOverride).toEqual({ mode: "custom", before: [129_600_000, 5_400_000] });
  expect(timed.createdAt).toBeInstanceOf(Date);
  expect(timed.completedAt).toBeNull();

  const allDay = await rest.updateItem(timed.id, { dueAt: null, dueDate: "2030-03-02" });
  expect(allDay.dueDate).toBe("2030-03-02");
  expect(allDay.allDay).toBe(true);
  expect(allDay.dueAt).toBeInstanceOf(Date);

  const sub = await rest.createSubitem(timed.id, { title: "print" });
  expect(sub.itemId).toBe(timed.id);
  const done = await rest.updateItem(timed.id, { status: "done" });
  expect(done.completedAt).toBeInstanceOf(Date);
  await rest.deleteItem(timed.id);
  await expect(rest.getItem(timed.id)).rejects.toMatchObject({ status: 404 });
});

test("the schedule of an item comes back with dates and offsets in milliseconds", async () => {
  const { rest } = await session("mia");
  await rest.setReminderPlan("normal", { before: [86_400_000, 3_600_000], followUp: "never" });
  const item = await rest.createItem({ title: "plan", dueAt: new Date("2031-01-10T12:00:00Z") });
  const schedule = await rest.getItemSchedule(item.id);
  expect(schedule.map((entry) => [entry.kind, entry.offset])).toEqual([
    ["before", 86_400_000],
    ["before", 3_600_000],
    ["due", null],
  ]);
  expect(schedule[0]!.scheduledFor).toEqual(new Date("2031-01-09T12:00:00Z"));
  expect(schedule[0]!.fireAt).toBeInstanceOf(Date);
  const plans = await rest.listReminderPlans();
  expect(plans.find((plan) => plan.priority === "normal")).toMatchObject({
    before: [86_400_000, 3_600_000],
    followUp: "never",
    isDefault: false,
  });
});

test("the profile keeps a language, quiet hours and preferences as sent", async () => {
  const { rest, user } = await session("mia");
  const preferences = { theme: "dark", saved_views: [{ due_at: "soon" }] };
  const updated = await rest.updateUser({
    locale: "de-DE",
    quietHours: { start: "22:00", end: "07:00" },
    preferences,
  });
  expect(updated).toMatchObject({ id: user.id, locale: "de-DE", preferences });
  expect(updated.quietHours).toEqual({ start: "22:00", end: "07:00" });
  expect(await rest.getUser()).toEqual(updated);
});

test("the session client logs in, pages through users as admin and logs out", async () => {
  const { rest } = await admin();
  await session("one");
  await session("two");
  let count = 0;
  for await (const page of rest.pages((cursor) => rest.listUsers({ limit: 2, cursor }))) {
    expect(page.data.length).toBeLessThanOrEqual(2);
    count += page.data.length;
    if (count >= 4) break;
  }
  expect(count).toBeGreaterThanOrEqual(3);

  const mia = await session("mia");
  await mia.rest.logout();
  await expect(mia.rest.listLists()).rejects.toMatchObject({ status: 401 });
});
