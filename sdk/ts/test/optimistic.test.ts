import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { fromWire } from "../src/convert.js";
import type { CacheChange, Item, List } from "../src/index.js";
import { optimistic } from "../src/optimistic/index.js";
import { connectedClient, deferred } from "./connected.js";
import { settle } from "./fake-gateway.js";
import { change, wireItem, wireList, wirePlan, wireUser } from "./fixtures.js";

async function setup(...args: Parameters<typeof connectedClient>) {
  const made = await connectedClient(...args);
  const view = optimistic(made.client);
  const heard: CacheChange[] = [];
  view.subscribe((seen) => heard.push(seen));
  return { ...made, view, heard, mine: () => view.user()!, real: () => made.client.cache.user()! };
}

const complete = (item: Item): Item => ({
  ...item,
  status: "done",
  completedAt: new Date("2026-11-14T12:00:00Z"),
});
const rename = (title: string) => (item: Item) => ({ ...item, title });
const ids = (items: readonly { id: string }[]) => items.map((item) => item.id);

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

test("a change shows at once and the cache keeps the confirmed state", async () => {
  const { view, mine, real, heard, send } = await setup();
  const call = deferred<string>();
  let nonce = "";
  const applied = view.apply([{ kind: "item", id: "a", update: complete }], (given) => {
    nonce = given;
    return call.promise;
  });
  expect(nonce).toMatch(/^[A-Za-z0-9]{16}$/);
  expect(ids(mine().items())).toEqual(["b"]);
  expect(ids(mine().history())).toEqual(["a"]);
  expect(mine().item("a")!.status).toBe("done");
  expect(ids(real().items())).toEqual(["a", "b"]);
  expect(heard).toEqual([{ userId: "u1", kinds: ["items", "history"], cause: undefined }]);

  send(
    "item.updated",
    change(
      "u1",
      wireItem({ id: "a", status: "done", completed_at: "2026-11-14T12:00:00.5Z" }),
      nonce,
    ),
  );
  expect(mine().history()).toBe(real().history());
  expect(mine().items()).toBe(real().items());
  call.resolve("ok");
  expect(await applied).toBe("ok");
  expect(ids(mine().history())).toEqual(["a"]);
});

test("a refused call takes back exactly its own changes and rejects", async () => {
  const { view, mine } = await setup();
  const first = deferred<void>();
  const second = deferred<void>();
  const keeps = view.apply(
    [{ kind: "item", id: "a", update: rename("kept") }],
    () => first.promise,
  );
  const fails = view.apply(
    [
      { kind: "item", id: "a", update: (item) => ({ ...item, priority: "high" }) },
      { kind: "item", id: "b", remove: true },
    ],
    () => second.promise,
  );
  expect(mine().item("a")).toMatchObject({ title: "kept", priority: "high" });
  expect(ids(mine().items())).toEqual(["a"]);
  const refused = expect(fails).rejects.toThrow("no");
  second.reject(new Error("no"));
  await refused;
  expect(mine().item("a")).toMatchObject({ title: "kept", priority: "normal" });
  expect(ids(mine().items())).toEqual(["a", "b"]);
  first.resolve();
  await keeps;
});

test("the answer before the event does not flip the view back", async () => {
  const { view, mine, real, send } = await setup();
  let nonce = "";
  await view.apply([{ kind: "item", id: "a", update: rename("new") }], async (given) => {
    nonce = given;
  });
  expect(mine().item("a")!.title).toBe("new");
  expect(real().item("a")!.title).toBe("Hand in the report");
  await vi.advanceTimersByTimeAsync(4000);
  expect(mine().item("a")!.title).toBe("new");
  send("item.updated", change("u1", wireItem({ id: "a", title: "new" }), nonce));
  expect(mine().item("a")).toBe(real().item("a"));
  expect(mine().items()).toBe(real().items());
});

test("two quick changes to one item never show an older state", async () => {
  const { view, mine, send } = await setup();
  const nonces: string[] = [];
  const remember = async (nonce: string) => void nonces.push(nonce);
  const seen: string[] = [];
  view.subscribe(() => seen.push(`${mine().item("a")!.title}/${mine().item("a")!.priority}`));
  await view.apply([{ kind: "item", id: "a", update: rename("new") }], remember);
  await view.apply(
    [{ kind: "item", id: "a", update: (item) => ({ ...item, priority: "high" }) }],
    remember,
  );
  send("item.updated", change("u1", wireItem({ id: "a", title: "new" }), nonces[0]));
  send(
    "item.updated",
    change("u1", wireItem({ id: "a", title: "new", priority: "high" }), nonces[1]),
  );
  expect(seen).toEqual(["new/normal", "new/high", "new/high", "new/high"]);
});

test("someone else's event for the same item does not settle the change", async () => {
  const { view, mine, send } = await setup();
  const call = deferred<void>();
  const applied = view.apply(
    [{ kind: "item", id: "a", update: rename("mine") }],
    () => call.promise,
  );
  send("item.updated", change("u1", wireItem({ id: "a", title: "theirs", note: "n" }), "other"));
  expect(mine().item("a")).toMatchObject({ title: "mine", note: "n" });
  call.resolve();
  await applied;
  expect(mine().item("a")!.title).toBe("mine");
});

test("a change the hub has nothing to announce for ends when the state already matches", async () => {
  const { view, mine, real } = await setup();
  await view.apply(
    [{ kind: "item", id: "a", update: (item) => ({ ...item, title: item.title }) }],
    async () => {},
  );
  expect(mine().items()).toBe(real().items());
});

test("five seconds after the answer only the confirmed state counts", async () => {
  const { view, mine, real, heard } = await setup();
  const call = deferred<void>();
  const applied = view.apply(
    [{ kind: "item", id: "a", update: rename("lost") }],
    () => call.promise,
  );
  await vi.advanceTimersByTimeAsync(60_000);
  expect(mine().item("a")!.title).toBe("lost");
  call.resolve();
  await applied;
  await vi.advanceTimersByTimeAsync(4999);
  expect(mine().item("a")!.title).toBe("lost");
  heard.length = 0;
  await vi.advanceTimersByTimeAsync(1);
  expect(mine().item("a")).toBe(real().item("a"));
  expect(heard).toEqual([{ userId: "u1", kinds: ["items", "history"], cause: undefined }]);
});

test("a created item is replaced by the real one as soon as its event arrives", async () => {
  const { view, mine, real, send } = await setup();
  const temp = view.tempId();
  expect(temp).toMatch(/^tmp_[A-Za-z0-9]{16}$/);
  const draft = fromWire<Item>(wireItem({ id: temp, title: "fresh" }));
  const call = deferred<Item>();
  let nonce = "";
  const applied = view.apply([{ kind: "item", create: draft }], (given) => {
    nonce = given;
    return call.promise;
  });
  expect(ids(mine().items())).toEqual(["a", "b", temp]);
  expect(mine().item(temp)).toBe(draft);
  expect(view.resolveId(temp)).toBeUndefined();
  const counts: number[] = [];
  view.subscribe(() => counts.push(mine().items().length));
  send("item.created", change("u1", wireItem({ id: "real", title: "fresh" }), nonce));
  expect(counts).toEqual([3]);
  expect(ids(mine().items())).toEqual(["a", "b", "real"]);
  expect(view.resolveId(temp)).toBe("real");
  expect(mine().item(temp)).toBeUndefined();
  call.resolve(real().item("real")!);
  await applied;
  expect(mine().items()).toBe(real().items());
});

test("without the event, the answer names the real id once the draft is given up", async () => {
  const { view, mine, real, send } = await setup();
  const temp = view.tempId();
  let nonce = "";
  await view.apply(
    [{ kind: "item", create: fromWire<Item>(wireItem({ id: temp, title: "fresh" })) }],
    async (given) => {
      nonce = given;
      return fromWire<Item>(wireItem({ id: "real", title: "fresh" }));
    },
  );
  expect(view.resolveId(temp)).toBeUndefined();
  expect(ids(mine().items())).toEqual(["a", "b", temp]);
  await vi.advanceTimersByTimeAsync(5000);
  expect(view.resolveId(temp)).toBe("real");
  expect(mine().items()).toBe(real().items());
  send("item.created", change("u1", wireItem({ id: "real", title: "fresh" }), nonce));
  expect(mine().item("real")!.title).toBe("fresh");
});

test("a change to a draft follows it to its real id", async () => {
  const { view, mine, send } = await setup();
  const temp = view.tempId();
  let nonce = "";
  void view.apply(
    [{ kind: "item", create: fromWire<Item>(wireItem({ id: temp, title: "fresh" })) }],
    (given) => {
      nonce = given;
      return new Promise(() => {});
    },
  );
  void view.apply(
    [{ kind: "item", id: temp, update: rename("edited") }],
    () => new Promise(() => {}),
  );
  expect(mine().item(temp)!.title).toBe("edited");
  send("item.created", change("u1", wireItem({ id: "real", title: "fresh" }), nonce));
  expect(mine().item("real")!.title).toBe("edited");
});

function draftList(view: ReturnType<typeof optimistic>): { id: string; nonce: () => string } {
  const id = view.tempId();
  let nonce = "";
  const created = fromWire<List>(wireList({ id, name: "New", position: 1, is_inbox: false }));
  void view.apply([{ kind: "list", create: created }], (given) => {
    nonce = given;
    return new Promise(() => {});
  });
  return { id, nonce: () => nonce };
}

test("an item drafted into a draft list follows the list to its real id", async () => {
  const { view, mine, send } = await setup();
  const list = draftList(view);
  const item = view.tempId();
  void view.apply(
    [
      {
        kind: "item",
        create: fromWire<Item>(wireItem({ id: item, title: "fresh", list_id: list.id })),
      },
    ],
    () => new Promise(() => {}),
  );
  send(
    "list.created",
    change("u1", wireList({ id: "l9", name: "New", position: 1, is_inbox: false }), list.nonce()),
  );
  expect(mine().item(item)!.listId).toBe("l9");
});

test("an item moved into a draft list follows the list and settles once the hub moved it", async () => {
  const { view, mine, real, send } = await setup();
  const list = draftList(view);
  let nonce = "";
  void view.apply(
    [{ kind: "item", id: "a", update: (item) => ({ ...item, listId: list.id }) }],
    (given) => {
      nonce = given;
      return new Promise(() => {});
    },
  );
  send(
    "list.created",
    change("u1", wireList({ id: "l9", name: "New", position: 1, is_inbox: false }), list.nonce()),
  );
  expect(mine().item("a")!.listId).toBe("l9");
  send("item.updated", change("u1", wireItem({ id: "a", list_id: "l9" }), nonce));
  expect(mine().item("a")).toBe(real().item("a"));
});

test("a change to an object that disappears is dropped", async () => {
  const { view, mine, real, send } = await setup();
  void view.apply([{ kind: "item", id: "a", update: rename("x") }], () => new Promise(() => {}));
  send("item.deleted", change("u1", { id: "a" }, "other"));
  expect(mine().items()).toBe(real().items());
  expect(mine().item("a")).toBeUndefined();

  void view.apply([{ kind: "item", id: "b", update: rename("x") }], () => new Promise(() => {}));
  send("user.gone", { user_id: "u1" });
  expect(view.user()).toBeUndefined();
  expect(view.users()).toEqual([]);
});

test("without pending changes the view hands out the cache's own state", async () => {
  const { view, mine, real } = await setup();
  expect(mine().items()).toBe(real().items());
  expect(mine().history()).toBe(real().history());
  expect(mine().lists()).toBe(real().lists());
  expect(mine().plans()).toBe(real().plans());
  expect(mine().profile).toBe(real().profile);
  expect(mine().historyCursor).toBeNull();
  expect(mine().userId).toBe("u1");
  expect(view.user("u1")).toBe(mine());
  expect(view.users()).toEqual([mine()]);
  expect(view.user("nobody")).toBeUndefined();
});

test("unaffected objects and arrays keep their identity while a change is pending", async () => {
  const { view, mine, real, send } = await setup();
  void view.apply([{ kind: "item", id: "a", update: rename("x") }], () => new Promise(() => {}));
  const items = mine().items();
  const changed = mine().item("a");
  expect(mine().items()).toBe(items);
  expect(mine().item("b")).toBe(real().item("b"));
  expect(mine().lists()).toBe(real().lists());
  send("list.updated", change("u1", wireList({ name: "Home" })));
  expect(mine().items()).toBe(items);
  send("item.updated", change("u1", wireItem({ id: "b", title: "other" }), "other"));
  expect(mine().items()).not.toBe(items);
  expect(mine().item("a")).toBe(changed);
});

test("lists, plans and the profile can be changed ahead as well", async () => {
  const { view, mine, real, send } = await setup({
    lists: [wireList(), wireList({ id: "l2", name: "Uni", position: 1, is_inbox: false })],
  });
  const temp = view.tempId();
  const nonces: string[] = [];
  const hold = (nonce: string) => {
    nonces.push(nonce);
    return new Promise<void>(() => {});
  };
  void view.apply(
    [
      { kind: "list", id: "l2", update: (list) => ({ ...list, position: 0 }) },
      { kind: "list", id: "l1", update: (list) => ({ ...list, position: 1 }) },
      {
        kind: "list",
        create: fromWire<List>(wireList({ id: temp, name: "New", position: 2, is_inbox: false })),
      },
    ],
    hold,
  );
  void view.apply(
    [{ kind: "plan", priority: "high", update: (plan) => ({ ...plan, before: [60_000] }) }],
    hold,
  );
  void view.apply(
    [{ kind: "profile", update: (profile) => ({ ...profile, displayName: "M" }) }],
    hold,
  );
  expect(ids(mine().lists())).toEqual(["l2", "l1", temp]);
  expect(mine().list(temp)!.name).toBe("New");
  expect(mine().plan("high")!.before).toEqual([60_000]);
  expect(
    mine()
      .plans()
      .map((plan) => plan.priority),
  ).toEqual(["low", "normal", "high"]);
  expect(mine().plan("low")).toBe(real().plan("low"));
  expect(mine().profile!.displayName).toBe("M");
  expect(real().profile!.displayName).toBe("Mia");

  send("plan.updated", change("u1", wirePlan({ priority: "high", before: ["1m"] }), nonces[1]));
  expect(mine().plans()).toBe(real().plans());
  send("user.updated", change("u1", wireUser({ display_name: "M" }), nonces[2]));
  expect(mine().profile).toBe(real().profile);
  send(
    "list.updated",
    change("u1", wireList({ id: "l2", name: "Uni", position: 0, is_inbox: false }), nonces[0]),
  );
  send("list.updated", change("u1", wireList({ position: 1 }), nonces[0]));
  send(
    "list.created",
    change("u1", wireList({ id: "l3", name: "New", position: 2, is_inbox: false }), nonces[0]),
  );
  expect(mine().lists()).toBe(real().lists());
  expect(view.resolveId(temp)).toBe("l3");
});

test("without the history intent an item completed ahead just leaves", async () => {
  const { view, mine } = await setup({}, ["users", "lists", "items", "plans"]);
  void view.apply([{ kind: "item", id: "a", update: complete }], () => new Promise(() => {}));
  expect(ids(mine().items())).toEqual(["b"]);
  expect(mine().history()).toEqual([]);
});

test("an item completed ahead takes its place in the history, a reopened one returns", async () => {
  const { view, mine } = await setup({
    history: [
      wireItem({ id: "h1", status: "done", completed_at: "2026-11-14T13:00:00Z" }),
      wireItem({ id: "h2", status: "done", completed_at: "2026-11-14T11:00:00Z" }),
    ],
  });
  void view.apply([{ kind: "item", id: "a", update: complete }], () => new Promise(() => {}));
  void view.apply(
    [
      {
        kind: "item",
        id: "h2",
        update: (item) => ({ ...item, status: "open", completedAt: null }),
      },
    ],
    () => new Promise(() => {}),
  );
  expect(ids(mine().history())).toEqual(["h1", "a"]);
  expect(ids(mine().items())).toEqual(["b", "h2"]);
});

test("a connection says which user a change is for", async () => {
  const { view } = await setup();
  await expect(
    view.apply([{ kind: "item", id: "a", update: rename("x") }], async () => {}, { user: "u9" }),
  ).resolves.toBeUndefined();
  expect(view.user("u1")!.item("a")!.title).toBe("Hand in the report");
});

test("the same client always gets the same view", async () => {
  const { client, view } = await setup();
  expect(optimistic(client)).toBe(view);
});

test("subscribers hear cache changes with their cause, and can unsubscribe", async () => {
  const { view, heard, send } = await setup();
  const off = view.subscribe(() => {
    throw new Error("a listener that throws does not stop the others");
  });
  const quiet = vi.spyOn(console, "error").mockImplementation(() => {});
  send("item.updated", change("u1", wireItem({ id: "a", title: "t" }), "n"));
  off();
  quiet.mockRestore();
  expect(heard).toEqual([
    { userId: "u1", kinds: ["items"], cause: { name: "item.updated", nonce: "n", id: "a" } },
  ]);
});

test("events with 5000 items and a pending change are absorbed quickly", async () => {
  vi.useRealTimers();
  const items = Array.from({ length: 5000 }, (_, i) => wireItem({ id: `i${i}` }));
  const { view, mine, send } = await setup({ items });
  void view.apply([{ kind: "item", id: "i7", update: rename("x") }], () => new Promise(() => {}));
  let slowest = 0;
  for (let i = 0; i < 100; i++) {
    const started = performance.now();
    send("item.updated", change("u1", wireItem({ id: `i${i + 100}`, title: "t" }), "other"));
    expect(mine().items()).toHaveLength(5000);
    slowest = Math.max(slowest, performance.now() - started);
  }
  // The dashboard needs 50 ms; the margin keeps a busy CI runner from failing this.
  expect(slowest).toBeLessThan(200);
  await settle();
});

test("a change back to the old value does not uncover the change before it", async () => {
  const { view, mine, send } = await setup();
  const nonces: string[] = [];
  const remember = async (nonce: string) => void nonces.push(nonce);
  const seen: string[] = [];
  view.subscribe(() => seen.push(mine().item("a")!.title));
  await view.apply([{ kind: "item", id: "a", update: rename("y") }], remember);
  await view.apply([{ kind: "item", id: "a", update: rename("Hand in the report") }], remember);
  expect(mine().item("a")!.title).toBe("Hand in the report");
  send("item.updated", change("u1", wireItem({ id: "a", title: "y" }), nonces[0]));
  send("item.updated", change("u1", wireItem({ id: "a" }), nonces[1]));
  expect(seen).toEqual(["y", "Hand in the report", "Hand in the report", "Hand in the report"]);
});

test("a change to a draft survives the answer coming before the event", async () => {
  const { view, mine, send } = await setup();
  const temp = view.tempId();
  const call = deferred<{ id: string }>();
  let nonce = "";
  const created = view.apply(
    [{ kind: "item", create: fromWire<Item>(wireItem({ id: temp, title: "fresh" })) }],
    (given) => {
      nonce = given;
      return call.promise;
    },
  );
  void view.apply(
    [{ kind: "item", id: temp, update: rename("edited") }],
    () => new Promise(() => {}),
  );
  call.resolve({ id: "real" });
  await created;
  expect(view.resolveId(temp)).toBeUndefined();
  expect(mine().item(temp)!.title).toBe("edited");
  send("item.updated", change("u1", wireItem({ id: "b", title: "unrelated" }), "other"));
  expect(mine().item(temp)!.title).toBe("edited");
  send("item.created", change("u1", wireItem({ id: "real", title: "fresh" }), nonce));
  expect(mine().item(temp)).toBeUndefined();
  expect(view.resolveId(temp)).toBe("real");
  expect(mine().item("real")!.title).toBe("edited");
});

test("two drafts in one call each get their own real object", async () => {
  const { view, mine, send } = await setup();
  const [first, second] = [view.tempId(), view.tempId()];
  let nonce = "";
  void view.apply(
    [
      { kind: "item", create: fromWire<Item>(wireItem({ id: first, title: "one" })) },
      { kind: "item", create: fromWire<Item>(wireItem({ id: second, title: "two" })) },
    ],
    (given) => {
      nonce = given;
      return new Promise(() => {});
    },
  );
  send("item.created", change("u1", wireItem({ id: "r1", title: "one" }), nonce));
  expect(ids(mine().items())).toEqual(["a", "b", "r1", second]);
  send("item.created", change("u1", wireItem({ id: "r2", title: "two" }), nonce));
  expect(ids(mine().items())).toEqual(["a", "b", "r1", "r2"]);
  expect([view.resolveId(first), view.resolveId(second)]).toEqual(["r1", "r2"]);
});

test("@me names the caller's own user in apply", async () => {
  const { view, mine } = await setup();
  void view.apply([{ kind: "item", id: "a", update: rename("x") }], () => new Promise(() => {}), {
    user: "@me",
  });
  expect(mine().item("a")!.title).toBe("x");
});

test("the history keeps its identity while only open items change under a pending change", async () => {
  const { view, mine, send } = await setup({
    history: [wireItem({ id: "h1", status: "done", completed_at: "2026-11-14T13:00:00Z" })],
  });
  void view.apply([{ kind: "item", id: "a", update: rename("x") }], () => new Promise(() => {}));
  const history = mine().history();
  const lists = mine().lists();
  send("item.updated", change("u1", wireItem({ id: "b", title: "other" }), "other"));
  expect(mine().history()).toBe(history);
  expect(mine().lists()).toBe(lists);
});

test("an update function that throws is skipped and does not break the view", async () => {
  const { view, mine, send } = await setup();
  const quiet = vi.spyOn(console, "error").mockImplementation(() => {});
  void view.apply(
    [
      {
        kind: "item",
        id: "a",
        update: () => {
          throw new Error("bad update");
        },
      },
    ],
    () => new Promise(() => {}),
  );
  expect(mine().item("a")!.title).toBe("Hand in the report");
  send("item.updated", change("u1", wireItem({ id: "b", title: "other" }), "other"));
  expect(mine().item("b")!.title).toBe("other");
  quiet.mockRestore();
});

test("two drafts in one call find their own object when the events arrive the other way round", async () => {
  const { view, mine, send } = await setup();
  const [first, second] = [view.tempId(), view.tempId()];
  let nonce = "";
  void view.apply(
    [
      { kind: "item", create: fromWire<Item>(wireItem({ id: first, title: "one" })) },
      { kind: "item", create: fromWire<Item>(wireItem({ id: second, title: "two" })) },
      { kind: "item", id: second, update: rename("two, edited") },
    ],
    (given) => {
      nonce = given;
      return new Promise(() => {});
    },
  );
  send("item.created", change("u1", wireItem({ id: "r2", title: "two" }), nonce));
  expect(view.resolveId(second)).toBe("r2");
  expect(view.resolveId(first)).toBeUndefined();
  expect(mine().item("r2")!.title).toBe("two, edited");
  send("item.created", change("u1", wireItem({ id: "r1", title: "one" }), nonce));
  expect(view.resolveId(first)).toBe("r1");
  expect(mine().item("r1")!.title).toBe("one");
});

test("a done item completed again ahead moves to its new place in the history", async () => {
  const { view, mine } = await setup({
    history: [
      wireItem({ id: "h1", status: "done", completed_at: "2026-11-14T13:00:00Z" }),
      wireItem({ id: "h2", status: "done", completed_at: "2026-11-14T11:00:00Z" }),
    ],
  });
  void view.apply(
    [
      {
        kind: "item",
        id: "h2",
        update: (item) => ({ ...item, completedAt: new Date("2026-11-14T14:00:00Z") }),
      },
    ],
    () => new Promise(() => {}),
  );
  expect(ids(mine().history())).toEqual(["h2", "h1"]);
});

test("an update function that throws does not fail a call that went through", async () => {
  const { view, mine, heard } = await setup();
  const quiet = vi.spyOn(console, "error").mockImplementation(() => {});
  const applied = view.apply(
    [
      {
        kind: "item",
        id: "a",
        update: () => {
          throw new Error("bad update");
        },
      },
    ],
    async () => "ok",
  );
  expect(await applied).toBe("ok");
  const before = heard.length;
  await vi.advanceTimersByTimeAsync(5000);
  expect(heard).toHaveLength(before);
  expect(mine().item("a")!.title).toBe("Hand in the report");
  quiet.mockRestore();
});

test("identical drafts are told apart by the nonce each create is sent with", async () => {
  const { view, mine, send } = await setup();
  const [first, second] = [view.tempId(), view.tempId()];
  const nonces: string[] = [];
  void view.apply(
    [
      { kind: "item", create: fromWire<Item>(wireItem({ id: first, title: "same" })) },
      { kind: "item", create: fromWire<Item>(wireItem({ id: second, title: "same" })) },
      { kind: "item", id: first, update: rename("first") },
      { kind: "item", id: second, update: rename("second") },
    ],
    (nonce, nonceFor) => {
      nonces.push(nonce, nonceFor(first), nonceFor(second));
      expect(() => nonceFor("tmp_unknown")).toThrow(TypeError);
      return new Promise(() => {});
    },
  );
  expect(new Set(nonces).size).toBe(3);
  send("item.created", change("u1", wireItem({ id: "r2", title: "same" }), nonces[2]));
  expect(view.resolveId(second)).toBe("r2");
  expect(view.resolveId(first)).toBeUndefined();
  expect(mine().item("r2")!.title).toBe("second");
  send("item.created", change("u1", wireItem({ id: "r1", title: "same" }), nonces[1]));
  expect(view.resolveId(first)).toBe("r1");
  expect(mine().item("r1")!.title).toBe("first");
});
