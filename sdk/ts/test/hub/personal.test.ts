import { afterEach, expect, test } from "vitest";
import { ApiError, GatewayError, type DeliveryCreatedEvent, type Item } from "../../src/index.js";
import {
  closeAll,
  completeInThePast,
  personalToken,
  restartHub,
  session,
  tokenClient,
  until,
} from "./helpers.js";

afterEach(closeAll);

const ids = (items: readonly { id: string }[]) => items.map((item) => item.id);

test("the cache after connect equals what REST says", async () => {
  const { rest } = await session("mia");
  const uni = await rest.createList({ name: "Uni" });
  const report = await rest.createItem({
    title: "Hand in the report",
    listId: uni.id,
    dueAt: new Date("2030-01-01T10:00:00Z"),
    subitems: ["print", "sign"],
    reminderOverride: { mode: "custom", before: [86_400_000, 3_600_000] },
  });
  await rest.createReminder(report.id, { at: new Date("2029-12-30T08:00:00Z") });
  await rest.createItem({ title: "Buy milk", dueDate: "2030-02-01", priority: "high" });
  const finished = await rest.createItem({ title: "Call home" });
  await rest.updateItem(finished.id, { status: "done" });
  await rest.setReminderPlan("high", { before: [7_200_000], followUp: "daily" });

  const { client } = tokenClient(await personalToken(rest));
  await client.connect();
  const state = client.cache.user()!;

  expect(state.lists()).toEqual(await rest.listLists());
  expect(state.plans()).toEqual(await rest.listReminderPlans());
  const open = await rest.listItems({ status: "open" });
  expect([...state.items()].sort(byId)).toEqual([...open.data].sort(byId));
  const done = await rest.listItems({ status: "done", sort: "completed", order: "desc" });
  expect(state.history()).toEqual(done.data);
  expect(state.historyCursor).toBeNull();
  expect(state.profile).toEqual(await rest.getUser());
  expect(state.item(report.id)!.reminderOverride).toEqual({
    mode: "custom",
    before: [86_400_000, 3_600_000],
  });
  expect(client.principal).toMatchObject({ kind: "personal_token", userId: state.userId });
});

function byId(a: Item, b: Item): number {
  return a.id < b.id ? -1 : 1;
}

test("a change made over REST arrives as an event with its nonce and is in the cache", async () => {
  const { rest } = await session("mia");
  const { client } = tokenClient(await personalToken(rest));
  await client.connect();
  const seen: string[] = [];
  client.on("item.created", (event) => {
    seen.push(`created ${event.nonce} ${event.actor?.kind} ${event.old === undefined}`);
  });
  client.on("item.updated", (event) => {
    seen.push(`updated ${event.nonce} ${event.old?.title} -> ${event.data.title}`);
  });
  const item = await rest.createItem({ title: "first" }, { nonce: "n-create" });
  await rest.updateItem(item.id, { title: "second" }, { nonce: "n-update" });
  await until("both events", () => seen.length === 2);
  expect(seen).toEqual(["created n-create session true", "updated n-update first -> second"]);
  expect(client.cache.user()!.item(item.id)).toEqual(await rest.getItem(item.id));
});

test("an event made with the client's own token names it as the actor", async () => {
  const { rest } = await session("mia");
  const { client } = tokenClient(await personalToken(rest));
  await client.connect();
  const actors: (string | undefined)[] = [];
  client.on("list.created", (event) => actors.push(event.actor?.id));
  await client.rest.createList({ name: "Mine" });
  await rest.createList({ name: "From the dashboard" });
  await until("both lists", () => actors.length === 2);
  expect(actors[0]).toBe(client.principal!.id);
  expect(actors[1]).not.toBe(client.principal!.id);
});

test("history continues over REST exactly where the sync ends", async () => {
  const { rest, user } = await session("mia");
  await completeInThePast(user.id, 1030);
  const { client } = tokenClient(await personalToken(rest));
  await client.connect();
  const state = client.cache.user()!;
  expect(state.history()).toHaveLength(1000);
  expect(state.historyCursor).not.toBeNull();

  const older: Item[] = [];
  const pages = client.rest.pages((cursor) =>
    client.rest.listItems({
      status: "done",
      sort: "completed",
      order: "desc",
      limit: 100,
      cursor: cursor ?? state.historyCursor!,
    }),
  );
  for await (const page of pages) older.push(...page.data);
  expect(older).toHaveLength(30);
  const all = new Set([...ids(state.history()), ...ids(older)]);
  expect(all.size).toBe(1030);
  expect(older[0]!.completedAt!.getTime()).toBeLessThanOrEqual(
    state.history().at(-1)!.completedAt!.getTime(),
  );

  const finished = await rest.createItem({ title: "just now" });
  await rest.updateItem(finished.id, { status: "done" });
  await until("the completed item", () => state.history()[0]?.id === finished.id);
  expect(state.history()).toHaveLength(1001);
});

test("a dropped connection is resumed and misses nothing", async () => {
  const { rest } = await session("mia");
  const { client, tap } = tokenClient(await personalToken(rest));
  await client.connect();
  const seen: string[] = [];
  client.on("disconnected", () => seen.push("disconnected"));
  client.on("resumed", () => seen.push("resumed"));
  client.on("ready", () => seen.push("ready"));
  tap.dropCurrent();
  await until("the drop", () => seen.includes("disconnected"));
  const item = await rest.createItem({ title: "while away" });
  await until("the resume", () => seen.includes("resumed"));
  expect(seen).toEqual(["disconnected", "resumed"]);
  expect(client.cache.user()!.item(item.id)!.title).toBe("while away");
  expect(tap.opened()).toBe(2);
});

test("after a hub restart the client identifies again and ends up with the full state", async () => {
  const { rest } = await session("mia");
  const { client } = tokenClient(await personalToken(rest));
  const before = await rest.createItem({ title: "before the restart" });
  await client.connect();
  let readies = 0;
  client.on("ready", () => readies++);
  const state = client.cache.user()!;
  await restartHub();
  const during = await rest.createItem({ title: "during the restart" });
  await until("the new session", () => readies === 1, 30_000);
  expect(client.cache.user()).toBe(state);
  expect(ids(state.items()).sort()).toEqual([before.id, during.id].sort());
  expect(client.status).toBe("connected");
});

test("a reminder arrives, is confirmed and shows as delivered in the log", async () => {
  const { rest } = await session("mia");
  const { client } = tokenClient(await personalToken(rest, true), ["deliveries"]);
  await client.connect();
  expect(client.cache.user()).toBeUndefined();
  const deliveries: DeliveryCreatedEvent[] = [];
  client.on("delivery.created", (event) => deliveries.push(event));
  const item = await rest.createItem({ title: "remind me" });
  await rest.createReminder(item.id, { at: new Date(Date.now() + 2000) });
  const event = await until("the reminder", () => deliveries[0], 25_000);
  expect(event.data).toMatchObject({
    attempt: 1,
    reminder: { kind: "manual" },
    item: { id: item.id, title: "remind me" },
  });
  expect(event.data.reminder.scheduledFor).toBeInstanceOf(Date);
  expect(event.nonce).toBeNull();

  const result = await client.rest.acknowledgeDelivery(event.data.id, { status: "delivered" });
  expect(result.status).toBe("delivered");
  const log = await rest.listReminderLog({ itemId: item.id });
  expect(log.data[0]!.deliveries[0]!.status).toBe("delivered");
  expect(log.data[0]!.firedAt).toBeInstanceOf(Date);
});

test("an unconfirmed reminder comes again after a reconnect, and a closed one cannot be confirmed", async () => {
  const { rest } = await session("mia");
  const { client, tap } = tokenClient(await personalToken(rest, true), ["deliveries"]);
  await client.connect();
  const attempts: number[] = [];
  const closed: string[] = [];
  let deliveryId = "";
  client.on("delivery.created", (event) => {
    deliveryId = event.data.id;
    attempts.push(event.data.attempt);
  });
  client.on("delivery.closed", (event) => closed.push(event.data.status));
  const item = await rest.createItem({ title: "remind me twice" });
  await rest.createReminder(item.id, { at: new Date(Date.now() + 2000) });
  await until("the reminder", () => attempts.length === 1, 25_000);
  tap.dropCurrent();
  await until("the reminder again", () => attempts.length === 2);
  expect(attempts).toEqual([1, 2]);
  expect((await client.rest.listDeliveries()).map((delivery) => delivery.id)).toEqual([deliveryId]);

  await rest.updateItem(item.id, { status: "done" });
  await until("the withdrawal", () => closed.length === 1);
  expect(closed).toEqual(["cancelled"]);
  const error = await client.rest
    .acknowledgeDelivery(deliveryId, { status: "delivered" })
    .catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  expect(error).toMatchObject({ status: 409, code: "delivery_closed" });
});

test("a deleted token ends the client with 4004", async () => {
  const { rest } = await session("mia");
  const token = await rest.createToken({ name: "short-lived" });
  const { client } = tokenClient(token.token!);
  await client.connect();
  const closed: (number | undefined)[] = [];
  client.on("closed", (event) => closed.push(event.code));
  await rest.deleteToken(token.id);
  await until("the end", () => closed.length === 1);
  expect(closed).toEqual([4004]);
  expect(client.status).toBe("closed");
});

test("a token that does not exist is refused by connect", async () => {
  const { client } = tokenClient("plsr_pat_this-token-does-not-exist");
  const error = await client.connect().catch((e: unknown) => e);
  expect(error).toBeInstanceOf(GatewayError);
});

test("the dashboard's login works as a gateway client too", async () => {
  const mia = await session("mia");
  const { client } = mia.client();
  await client.connect();
  expect(client.principal).toMatchObject({ kind: "session", userId: mia.user.id });
  const item = await client.rest.createItem({ title: "from the session" });
  await until("the item", () => client.cache.user()!.item(item.id));
  expect(client.cache.user()!.profile).toMatchObject({ id: mia.user.id, preferences: {} });
});
