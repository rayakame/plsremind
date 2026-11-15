import { ApiError } from "plsremind";
import { expect, test } from "vitest";
import {
  createItem,
  createList,
  moveList,
  patchItem,
  removeItem,
  removeList,
  renameList,
  setDone,
} from "../src/hub/write.js";
import { change, connectedHub, deferred, settle, wireItem, wireList } from "./support.js";

const ids = (all: readonly { id: string }[]) => all.map((one) => one.id);

test("completing shows at once and tells the hub with a nonce", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  hub.route("PATCH", "/users/@me/items/a", () => answer.promise);
  const done = setDone(hub.own, hub.mine().item("a")!, true);
  expect(ids(hub.mine().items())).toEqual(["b"]);
  expect(ids(hub.mine().history())).toEqual(["a"]);
  await Promise.resolve();
  const call = hub.sent("PATCH", "/users/@me/items/a")[0]!;
  expect(call.body).toEqual({ status: "done" });
  expect(call.headers["x-request-nonce"]).toMatch(/^[A-Za-z0-9]{16}$/);
  answer.resolve({ status: 200, body: wireItem({ id: "a", status: "done" }) });
  await done;
});

test("a refused change is taken back and the hub's error comes out", async () => {
  const hub = await connectedHub();
  hub.route("PATCH", "/users/@me/items/a", {
    status: 404,
    body: { error: { code: "not_found", message: "The resource does not exist." } },
  });
  const failure = await setDone(hub.own, hub.mine().item("a")!, true).catch((e: unknown) => e);
  expect(failure).toBeInstanceOf(ApiError);
  expect((failure as ApiError).code).toBe("not_found");
  expect(ids(hub.mine().items())).toEqual(["a", "b"]);
});

test("a new item shows as a draft in the inbox and resolves to the real one", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  hub.route("POST", "/users/@me/items", () => answer.promise);
  const created = createItem(hub.own, { title: "Fresh" });
  const draft = hub.mine().items().at(-1)!;
  expect(draft.id).toMatch(/^tmp_/);
  expect(draft).toMatchObject({ title: "Fresh", listId: "l1", priority: "normal", status: "open" });
  await Promise.resolve();
  const call = hub.sent("POST", "/users/@me/items")[0]!;
  expect(call.body).toEqual({ title: "Fresh", list_id: "l1" });
  const real = wireItem({ id: "real", title: "Fresh" });
  await hub.send("item.created", change("u1", real, call.headers["x-request-nonce"]!));
  answer.resolve({ status: 201, body: real });
  expect(await created).toBe("real");
  expect(ids(hub.mine().items())).toEqual(["a", "b", "real"]);
});

test("a due day or time is sent the way the hub wants it, in the profile's zone", async () => {
  const hub = await connectedHub();
  hub.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "x" }) });
  hub.route("PATCH", "/users/@me/items/a", { status: 200, body: wireItem({ id: "a" }) });
  void createItem(hub.own, {
    title: "Whole day",
    listId: "l2",
    priority: "high",
    due: { day: "2026-11-20", time: null },
  });
  expect(hub.mine().items().at(-1)).toMatchObject({
    allDay: true,
    dueDate: "2026-11-20",
    dueAt: new Date("2026-11-20T22:59:59Z"),
  });
  await Promise.resolve();
  expect(hub.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Whole day",
    list_id: "l2",
    priority: "high",
    due_date: "2026-11-20",
  });

  void patchItem(hub.own, hub.mine().item("a")!, { due: { day: "2026-03-29", time: "12:00" } });
  expect(hub.mine().item("a")).toMatchObject({ allDay: false, dueDate: null });
  await Promise.resolve();
  expect(hub.sent("PATCH", "/users/@me/items/a")[0]!.body).toEqual({
    due_at: "2026-03-29T10:00:00.000Z",
  });

  void patchItem(hub.own, hub.mine().item("a")!, { due: null, title: "Renamed" });
  await Promise.resolve();
  expect(hub.sent("PATCH", "/users/@me/items/a")[1]!.body).toEqual({
    due_at: null,
    title: "Renamed",
  });
});

test("a change to a draft waits for the real id", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  hub.route("POST", "/users/@me/items", () => answer.promise);
  hub.route("PATCH", "/users/@me/items/real", { status: 200, body: wireItem({ id: "real" }) });
  const created = createItem(hub.own, { title: "Fresh" });
  const draft = hub.mine().items().at(-1)!;
  const done = setDone(hub.own, draft, true);
  expect(hub.mine().item(draft.id)!.status).toBe("done");
  await Promise.resolve();
  expect(hub.sent("PATCH", /items/)).toHaveLength(0);
  const nonce = hub.sent("POST", "/users/@me/items")[0]!.headers["x-request-nonce"]!;
  await hub.send("item.created", change("u1", wireItem({ id: "real", title: "Fresh" }), nonce));
  answer.resolve({ status: 201, body: wireItem({ id: "real", title: "Fresh" }) });
  await created;
  await done;
  expect(hub.sent("PATCH", "/users/@me/items/real")[0]!.body).toEqual({ status: "done" });
});

test("items and lists are removed, renamed and moved ahead of the hub", async () => {
  const hub = await connectedHub({
    sync: {
      lists: [
        wireList(),
        wireList({ id: "l2", name: "Work", position: 1, is_inbox: false }),
        wireList({ id: "l3", name: "Home", position: 2, is_inbox: false }),
      ],
      items: [wireItem({ id: "a", list_id: "l2" }), wireItem({ id: "b" })],
    },
  });
  hub.route("DELETE", /./, { status: 204 });
  hub.route("PATCH", /./, { status: 200, body: wireList({ id: "l2" }) });
  hub.route("PUT", /./, { status: 200, body: [] });
  hub.route("POST", "/users/@me/lists", { status: 201, body: wireList({ id: "l9", name: "New" }) });

  void renameList(hub.own, hub.mine().list("l2")!, "Job");
  expect(hub.mine().list("l2")!.name).toBe("Job");

  void moveList(hub.own, hub.mine().list("l3")!, -1);
  expect(ids(hub.mine().lists())).toEqual(["l1", "l3", "l2"]);
  await settle();
  expect(hub.sent("PUT", "/users/@me/lists/order")[0]!.body).toEqual({ ids: ["l1", "l3", "l2"] });

  void removeList(hub.own, hub.mine().list("l2")!);
  expect(ids(hub.mine().lists())).toEqual(["l1", "l3"]);
  expect(hub.mine().item("a")!.listId).toBe("l1");

  void removeItem(hub.own, hub.mine().item("b")!);
  expect(ids(hub.mine().items())).toEqual(["a"]);

  void createList(hub.own, "New");
  expect(hub.mine().lists().at(-1)).toMatchObject({ name: "New", isInbox: false, position: 2 });
});

test("a change to a subitem that is still a draft waits for it and then names the real one", async () => {
  const hub = await connectedHub();
  const created = deferred<{ status: number; body: unknown }>();
  const real = { id: "s9", item_id: "a", title: "Outline", done: false, position: 0 };
  hub.route("POST", "/users/@me/items/a/subitems", () => created.promise);
  hub.route("PATCH", "/users/@me/items/a/subitems/s9", {
    status: 200,
    body: { ...real, done: true },
  });
  const { addSubitem, patchSubitem } = await import("../src/hub/write.js");

  void addSubitem(hub.own, hub.mine().item("a")!, "Outline");
  void addSubitem(hub.own, hub.mine().item("a")!, "Draft");
  const draft = hub.mine().item("a")!.subitems[0]!;
  void patchSubitem(hub.own, hub.mine().item("a")!, draft, { done: true });
  expect(
    hub
      .mine()
      .item("a")!
      .subitems.map((one) => `${one.title}:${one.done}`),
  ).toEqual(["Outline:true", "Draft:false"]);
  await settle();
  expect(hub.sent("POST", "/users/@me/items/a/subitems")).toHaveLength(1);
  expect(hub.sent("PATCH", /subitems/)).toHaveLength(0);

  const nonce = hub.sent("POST", "/users/@me/items/a/subitems")[0]!.headers["x-request-nonce"]!;
  await hub.send("item.updated", change("u1", wireItem({ id: "a", subitems: [real] }), nonce));
  created.resolve({ status: 201, body: real });
  await settle();
  expect(
    hub
      .mine()
      .item("a")!
      .subitems.map((one) => `${one.id}:${one.done}`)[0],
  ).toBe("s9:true");
  expect(hub.sent("POST", "/users/@me/items/a/subitems")).toHaveLength(2);
  expect(hub.sent("PATCH", "/users/@me/items/a/subitems/s9")[0]!.body).toEqual({ done: true });
});
