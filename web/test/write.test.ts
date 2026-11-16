import { ApiError } from "plsremind";
import { expect, test, vi } from "vitest";
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
import {
  change,
  connectedHub,
  deferred,
  settle,
  wireItem,
  wireList,
  wirePlan,
  wireUser,
} from "./support.js";

const ids = (all: readonly { id: string }[]) => all.map((one) => one.id);

test("completing shows at once and tells the hub with a nonce", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  hub.route("PATCH", "/users/@me/items/a", () => answer.promise);
  const done = setDone(hub.own, hub.mine().item("a")!, true);
  expect(ids(hub.mine().items())).toEqual(["b"]);
  expect(ids(hub.mine().history())).toEqual(["a"]);
  await settle();
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
  await settle();
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
  await settle();
  expect(hub.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Whole day",
    list_id: "l2",
    priority: "high",
    due_date: "2026-11-20",
  });

  void patchItem(hub.own, hub.mine().item("a")!, { due: { day: "2026-03-29", time: "12:00" } });
  expect(hub.mine().item("a")).toMatchObject({ allDay: false, dueDate: null });
  await settle();
  expect(hub.sent("PATCH", "/users/@me/items/a")[0]!.body).toEqual({
    due_at: "2026-03-29T10:00:00.000Z",
  });

  void patchItem(hub.own, hub.mine().item("a")!, { due: null, title: "Renamed" });
  await settle();
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
  await settle();
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
  for (let i = 0; i < 5; i++) await settle();
  expect(
    hub
      .mine()
      .item("a")!
      .subitems.map((one) => `${one.id}:${one.done}`)[0],
  ).toBe("s9:true");
  expect(hub.sent("POST", "/users/@me/items/a/subitems")).toHaveLength(2);
  expect(hub.sent("PATCH", "/users/@me/items/a/subitems/s9")[0]!.body).toEqual({ done: true });
});

test("two changes to one item reach the hub in the order they were made", async () => {
  const hub = await connectedHub();
  const first = deferred<{ status: number; body: unknown }>();
  let asked = 0;
  hub.route("PATCH", "/users/@me/items/a", () =>
    ++asked === 1 ? first.promise : { status: 200, body: wireItem({ id: "a" }) },
  );
  hub.route("PATCH", "/users/@me/items/b", { status: 200, body: wireItem({ id: "b" }) });
  void patchItem(hub.own, hub.mine().item("a")!, { due: { day: "2030-01-15", time: null } });
  void patchItem(hub.own, hub.mine().item("a")!, { due: { day: "2030-01-15", time: "12:00" } });
  void patchItem(hub.own, hub.mine().item("b")!, { title: "Other" });
  await settle();
  expect(hub.sent("PATCH", "/users/@me/items/a").map((call) => call.body)).toEqual([
    { due_date: "2030-01-15" },
  ]);
  expect(hub.sent("PATCH", "/users/@me/items/b")).toHaveLength(1);
  first.resolve({ status: 200, body: wireItem({ id: "a" }) });
  await settle();
  expect(hub.sent("PATCH", "/users/@me/items/a").map((call) => call.body)).toEqual([
    { due_date: "2030-01-15" },
    { due_at: "2030-01-15T11:00:00.000Z" },
  ]);
});

test("changes to the preferences, the profile and the order of lists also go out in order", async () => {
  const hub = await connectedHub();
  const first = deferred<{ status: number; body: unknown }>();
  let asked = 0;
  hub.route("PATCH", "/users/@me", () =>
    ++asked === 1 ? first.promise : { status: 200, body: {} },
  );
  const { setPrefs, updateProfile } = await import("../src/hub/write.js");
  void setPrefs(hub.own, { theme: "dark" });
  void setPrefs(hub.own, { weekStart: "sunday" });
  void updateProfile(hub.own, { displayName: "M" });
  await settle();
  expect(hub.sent("PATCH", "/users/@me")).toHaveLength(1);
  first.resolve({ status: 200, body: {} });
  await settle();
  expect(hub.sent("PATCH", "/users/@me").map((call) => call.body)).toEqual([
    { preferences: { theme: "dark" } },
    { preferences: { theme: "dark", weekStart: "sunday" } },
    { display_name: "M" },
  ]);
});

test("the time of completion is fixed when the change is made", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  hub.route("PATCH", "/users/@me/items/a", () => answer.promise);
  const done = setDone(hub.own, hub.mine().item("a")!, true);
  const first = hub.mine().item("a");
  await hub.send("item.updated", change("u1", wireItem({ id: "b", title: "Other" })));
  expect(hub.mine().item("a")!.completedAt).toEqual(first!.completedAt);
  answer.resolve({ status: 200, body: wireItem({ id: "a", status: "done" }) });
  await done;
});

test("a call that never gets an answer gives up, is taken back and lets the next one through", async () => {
  vi.useFakeTimers();
  try {
    const hub = await connectedHub();
    let asked = 0;
    hub.route("PATCH", "/users/@me/items/a", (call) => {
      asked++;
      return asked === 1
        ? new Promise(() => {})
        : { status: 200, body: wireItem({ id: "a", ...(call.body as object) }) };
    });
    const stuck = patchItem(hub.own, hub.mine().item("a")!, { title: "Never arrives" });
    const failure = stuck.catch((error: unknown) => error);
    const next = patchItem(hub.own, hub.mine().item("a")!, { note: "Arrives" });
    await vi.advanceTimersByTimeAsync(29_000);
    expect(hub.sent("PATCH", "/users/@me/items/a")).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(2_000);
    expect((await failure) as Error).toBeInstanceOf(Error);
    await next;
    expect(hub.sent("PATCH", "/users/@me/items/a")).toHaveLength(2);
    expect(hub.mine().item("a")!.title).toBe("Hand in the report");
  } finally {
    vi.useRealTimers();
  }
});

test("changes made to a draft and to the item it became go out in one order", async () => {
  const hub = await connectedHub();
  const created = deferred<{ status: number; body: unknown }>();
  const first = deferred<{ status: number; body: unknown }>();
  hub.route("POST", "/users/@me/items", () => created.promise);
  let asked = 0;
  hub.route("PATCH", "/users/@me/items/real", () =>
    ++asked === 1 ? first.promise : { status: 200, body: wireItem({ id: "real" }) },
  );
  const making = createItem(hub.own, { title: "Fresh" });
  const draft = hub.mine().items().at(-1)!;
  void patchItem(hub.own, draft, { title: "One" });
  await settle();
  const nonce = hub.sent("POST", "/users/@me/items")[0]!.headers["x-request-nonce"]!;
  await hub.send("item.created", change("u1", wireItem({ id: "real", title: "Fresh" }), nonce));
  created.resolve({ status: 201, body: wireItem({ id: "real", title: "Fresh" }) });
  await making;
  await settle();
  void patchItem(hub.own, hub.mine().item("real")!, { title: "Two" });
  await settle();
  expect(hub.sent("PATCH", "/users/@me/items/real").map((call) => call.body)).toEqual([
    { title: "One" },
  ]);
  first.resolve({ status: 200, body: wireItem({ id: "real" }) });
  await settle();
  expect(hub.sent("PATCH", "/users/@me/items/real").map((call) => call.body)).toEqual([
    { title: "One" },
    { title: "Two" },
  ]);
});

test("a preference the hub refused is not carried along by the next change", async () => {
  const hub = await connectedHub();
  let asked = 0;
  hub.route("PATCH", "/users/@me", (call) =>
    ++asked === 1
      ? { status: 500, body: { error: { code: "internal", message: "no" } } }
      : { status: 200, body: wireUser({ locale: "en", ...(call.body as object) }) },
  );
  const { setPrefs } = await import("../src/hub/write.js");
  const refused = setPrefs(hub.own, { theme: "dark" }).catch(() => "refused");
  const kept = setPrefs(hub.own, { weekStart: "sunday" });
  expect(await refused).toBe("refused");
  await kept;
  expect(hub.sent("PATCH", "/users/@me").map((call) => call.body)).toEqual([
    { preferences: { theme: "dark" } },
    { preferences: { weekStart: "sunday" } },
  ]);
});

test("deleting a draft waits for it to become real instead of giving up at the next change", async () => {
  const hub = await connectedHub();
  const created = deferred<{ status: number; body: unknown }>();
  hub.route("POST", "/users/@me/items", () => created.promise);
  hub.route("DELETE", "/users/@me/items/real", { status: 204 });
  const making = createItem(hub.own, { title: "Fresh" });
  const draft = hub.mine().items().at(-1)!;
  const removing = removeItem(hub.own, draft);
  await hub.send("item.updated", change("u1", wireItem({ id: "b", title: "Unrelated" })));
  await settle();
  const nonce = hub.sent("POST", "/users/@me/items")[0]!.headers["x-request-nonce"]!;
  await hub.send("item.created", change("u1", wireItem({ id: "real", title: "Fresh" }), nonce));
  created.resolve({ status: 201, body: wireItem({ id: "real", title: "Fresh" }) });
  await making;
  await removing;
  expect(hub.sent("DELETE", "/users/@me/items/real")).toHaveLength(1);
});

test("an answered preference that a later one replaced does not come back", async () => {
  const hub = await connectedHub();
  hub.route("PATCH", "/users/@me", (call) => ({
    status: 200,
    body: wireUser({ locale: "en", ...(call.body as object) }),
  }));
  const { setPrefs } = await import("../src/hub/write.js");
  await setPrefs(hub.own, { theme: "dark" });
  await setPrefs(hub.own, { theme: "light" });
  await hub.send(
    "user.updated",
    change("u1", wireUser({ locale: "en", preferences: { theme: "light" } })),
  );
  await setPrefs(hub.own, { weekStart: "sunday" });
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "light", weekStart: "sunday" },
  });
});

test("a preference changed on another device after ours is not undone by our next change", async () => {
  const hub = await connectedHub();
  hub.route("PATCH", "/users/@me", (call) => ({
    status: 200,
    body: wireUser({ locale: "en", ...(call.body as object) }),
  }));
  const { setPrefs } = await import("../src/hub/write.js");
  await setPrefs(hub.own, { theme: "dark" });
  const nonce = hub.sent("PATCH", "/users/@me")[0]!.headers["x-request-nonce"]!;
  await hub.send(
    "user.updated",
    change("u1", wireUser({ locale: "en", preferences: { theme: "dark" } }), nonce),
  );
  await hub.send(
    "user.updated",
    change("u1", wireUser({ locale: "en", preferences: { theme: "light" } })),
  );
  await setPrefs(hub.own, { weekStart: "sunday" });
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "light", weekStart: "sunday" },
  });
});

test("a refused part of a plan or of the quiet hours is not sent along with the next part", async () => {
  const hub = await connectedHub();
  let plans = 0;
  hub.route("PUT", "/users/@me/reminder-plans/normal", (call) =>
    ++plans === 1
      ? { status: 500, body: { error: { code: "internal", message: "no" } } }
      : { status: 200, body: wirePlan(call.body as Record<string, unknown>) },
  );
  let profiles = 0;
  hub.route("PATCH", "/users/@me", (call) =>
    ++profiles === 2
      ? { status: 500, body: { error: { code: "internal", message: "no" } } }
      : { status: 200, body: wireUser({ locale: "en", ...(call.body as object) }) },
  );
  const { setPlan, setQuiet } = await import("../src/hub/write.js");
  const refused = setPlan(hub.own, "normal", { before: [7_200_000] }).catch(() => "refused");
  const kept = setPlan(hub.own, "normal", { followUp: "daily" });
  expect(await refused).toBe("refused");
  await kept;
  expect(hub.sent("PUT", "/users/@me/reminder-plans/normal").at(-1)!.body).toEqual({
    before: ["1d"],
    follow_up: "daily",
  });

  await setQuiet(hub.own, { start: "22:00", end: "07:00" });
  const late = setQuiet(hub.own, { start: "23:00" }).catch(() => "refused");
  const end = setQuiet(hub.own, { end: "08:00" });
  expect(await late).toBe("refused");
  await end;
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    quiet_hours: { start: "22:00", end: "08:00" },
  });
});

const wholeState = (preferences: object) => ({
  user_id: "u1",
  chunk: { index: 0, count: 1 },
  user: wireUser({ locale: "en", preferences }),
  lists: [wireList()],
  plans: [wirePlan({ priority: "low" }), wirePlan(), wirePlan({ priority: "high" })],
  items: [],
  history: [],
  history_cursor: null,
});

test("a new sync while a preference is on its way decides what the hub holds", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  let asked = 0;
  hub.route("PATCH", "/users/@me", (call) =>
    ++asked === 1
      ? answer.promise
      : { status: 200, body: wireUser({ locale: "en", ...(call.body as object) }) },
  );
  const { setPrefs } = await import("../src/hub/write.js");
  const dark = setPrefs(hub.own, { theme: "dark" });
  await settle();
  await hub.send("user.sync", wholeState({ theme: "dark" }));
  await hub.send(
    "user.updated",
    change("u1", wireUser({ locale: "en", preferences: { theme: "light" } })),
  );
  answer.resolve({ status: 200, body: wireUser({ locale: "en", preferences: { theme: "dark" } }) });
  await dark;
  hub.setUser(wireUser({ locale: "en", preferences: { theme: "light" } }));
  await setPrefs(hub.own, { weekStart: "sunday" });
  expect(hub.sent("GET", "/users/@me")).toHaveLength(1);
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "light", weekStart: "sunday" },
  });
});

test("a sync from before a preference was stored does not lose it", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  let asked = 0;
  hub.route("PATCH", "/users/@me", (call) =>
    ++asked === 1
      ? answer.promise
      : { status: 200, body: wireUser({ locale: "en", ...(call.body as object) }) },
  );
  const { setPrefs } = await import("../src/hub/write.js");
  const dark = setPrefs(hub.own, { theme: "dark" });
  await settle();
  await hub.send("user.sync", wholeState({}));
  answer.resolve({ status: 200, body: wireUser({ locale: "en", preferences: { theme: "dark" } }) });
  await dark;
  hub.setUser(wireUser({ locale: "en", preferences: { theme: "dark" } }));
  await setPrefs(hub.own, { weekStart: "sunday" });
  expect(hub.sent("GET", "/users/@me")).toHaveLength(1);
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "dark", weekStart: "sunday" },
  });
  await setPrefs(hub.own, { prefix: "!" });
  expect(hub.sent("GET", "/users/@me")).toHaveLength(1);
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "dark", weekStart: "sunday", prefix: "!" },
  });
});

test("if the hub cannot be asked, the change fails and the question stays open", async () => {
  const hub = await connectedHub();
  const answer = deferred<{ status: number; body: unknown }>();
  let asked = 0;
  hub.route("PATCH", "/users/@me", (call) =>
    ++asked === 1
      ? answer.promise
      : { status: 200, body: wireUser({ locale: "en", ...(call.body as object) }) },
  );
  const { setPrefs } = await import("../src/hub/write.js");
  const dark = setPrefs(hub.own, { theme: "dark" });
  await settle();
  await hub.send("user.sync", wholeState({}));
  answer.resolve({ status: 200, body: wireUser({ locale: "en", preferences: { theme: "dark" } }) });
  await dark;
  hub.route("GET", "/users/@me", {
    status: 500,
    body: { error: { code: "internal", message: "no" } },
  });
  expect(await setPrefs(hub.own, { weekStart: "sunday" }).catch(() => "failed")).toBe("failed");
  expect(hub.sent("PATCH", "/users/@me")).toHaveLength(1);
  hub.route("GET", "/users/@me", {
    status: 200,
    body: wireUser({ locale: "en", preferences: { theme: "dark" } }),
  });
  await setPrefs(hub.own, { weekStart: "sunday" });
  expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
    preferences: { theme: "dark", weekStart: "sunday" },
  });
});

test("when the event for an answer does not come, the hub is asked before the next change", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  try {
    const hub = await connectedHub();
    hub.route("PATCH", "/users/@me", (call) => ({
      status: 200,
      body: wireUser({ locale: "en", ...(call.body as object) }),
    }));
    const { setPrefs } = await import("../src/hub/write.js");
    await setPrefs(hub.own, { theme: "dark" });
    vi.setSystemTime(Date.now() + 6000);
    hub.setUser(wireUser({ locale: "en", preferences: { theme: "light" } }));
    await setPrefs(hub.own, { weekStart: "sunday" });
    expect(hub.sent("GET", "/users/@me")).toHaveLength(1);
    expect(hub.sent("PATCH", "/users/@me").at(-1)!.body).toEqual({
      preferences: { theme: "light", weekStart: "sunday" },
    });
  } finally {
    vi.useRealTimers();
  }
});
