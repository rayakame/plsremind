import { expect, test, vi } from "vitest";
import { CacheStore } from "../src/cache.js";
import type { CacheChange, Intent, Item, List } from "../src/index.js";
import { change, sync, wireItem, wireList, wirePlan, wireUser } from "./fixtures.js";

const all: Intent[] = ["users", "lists", "items", "plans", "history", "deliveries"];
const kinds = ["profile", "lists", "items", "history", "plans"];

function cache(users = ["u1"], intents: Intent[] = all, self: string | null = "u1") {
  const store = new CacheStore();
  const changes: CacheChange[] = [];
  store.cache.subscribe((seen) => changes.push(seen));
  store.begin(users, intents, self ?? undefined);
  return { store, changes };
}

function loaded(parts: Record<string, unknown> = {}, intents: Intent[] = all) {
  const made = cache(["u1"], intents);
  made.store.apply(
    "user.sync",
    sync(
      "u1",
      { index: 0, count: 1 },
      {
        user: wireUser(),
        lists: [wireList()],
        plans: [wirePlan({ priority: "low" }), wirePlan(), wirePlan({ priority: "high" })],
        items: [wireItem({ id: "a" }), wireItem({ id: "b" })],
        ...(intents.includes("history") ? { history: [], history_cursor: null } : {}),
        ...parts,
      },
    ),
  );
  made.changes.length = 0;
  return { ...made, user: made.store.cache.user("u1")! };
}

function done(id: string, completedAt: string, more: Record<string, unknown> = {}) {
  return wireItem({ id, status: "done", completed_at: completedAt, ...more });
}

test("a sync in parts becomes visible in one step with the last part", () => {
  const { store, changes } = cache(["u1"], ["users", "lists", "items", "plans"]);
  const first = store.apply(
    "user.sync",
    sync(
      "u1",
      { index: 0, count: 2 },
      {
        user: wireUser(),
        lists: [wireList()],
        plans: [wirePlan()],
        items: [wireItem({ id: "a" })],
      },
    ),
  );
  expect(first.synced).toBeUndefined();
  expect(store.cache.user("u1")).toBeUndefined();
  const last = store.apply(
    "user.sync",
    sync("u1", { index: 1, count: 2 }, { items: [wireItem({ id: "b" })] }),
  );
  expect(last.synced).toBe("u1");
  const user = store.cache.user("u1")!;
  expect(user.items().map((item) => item.id)).toEqual(["a", "b"]);
  expect(user.profile).toMatchObject({ id: "u1", displayName: "Mia" });
  expect(user.lists()).toHaveLength(1);
  expect(user.plans()).toHaveLength(1);
  expect(user.history()).toEqual([]);
  expect(user.historyCursor).toBeNull();
  expect(changes).toEqual([{ userId: "u1", kinds, cause: undefined }]);
  expect(store.cache.users()).toEqual([user]);
});

test("arrays stay the same until their kind changes", () => {
  const { store, user } = loaded();
  const items = user.items();
  const untouched = user.item("b");
  expect(user.items()).toBe(items);
  store.apply("list.updated", change("u1", wireList({ name: "Home" })));
  expect(user.items()).toBe(items);
  store.apply("item.updated", change("u1", wireItem({ id: "a", title: "new" })));
  expect(user.items()).not.toBe(items);
  expect(user.items().map((item) => item.id)).toEqual(["a", "b"]);
  expect(user.item("b")).toBe(untouched);
  expect(Object.isFrozen(user.items())).toBe(true);
});

test("an update replaces the object and hands back the old one", () => {
  const { store, user, changes } = loaded();
  const before = user.item("a")!;
  const applied = store.apply(
    "item.updated",
    change("u1", wireItem({ id: "a", title: "new" }), "n1"),
  );
  expect(applied.old).toBe(before);
  expect(before.title).toBe("Hand in the report");
  expect(user.item("a")!.title).toBe("new");
  expect((applied.event as { data: Item }).data).toBe(user.item("a"));
  expect(changes).toEqual([
    { userId: "u1", kinds: ["items"], cause: { name: "item.updated", nonce: "n1", id: "a" } },
  ]);
});

test("a created item is added at the end", () => {
  const { store, user } = loaded();
  const applied = store.apply("item.created", change("u1", wireItem({ id: "c" })));
  expect(applied.old).toBeUndefined();
  expect(user.items().map((item) => item.id)).toEqual(["a", "b", "c"]);
});

test("a completed item moves to the history and back when reopened", () => {
  const { store, user, changes } = loaded();
  store.apply("item.updated", change("u1", done("a", "2026-11-10T10:00:00Z")));
  expect(user.items().map((item) => item.id)).toEqual(["b"]);
  expect(user.history().map((item) => item.id)).toEqual(["a"]);
  expect(changes[0]!.kinds).toEqual(["items", "history"]);
  store.apply("item.updated", change("u1", wireItem({ id: "a" })));
  expect(user.items().map((item) => item.id)).toEqual(["b", "a"]);
  expect(user.history()).toEqual([]);
  expect(changes[1]!.kinds).toEqual(["items", "history"]);
});

test("without the history intent a completed item just leaves", () => {
  const { store, user, changes } = loaded({}, ["users", "lists", "items", "plans"]);
  const applied = store.apply("item.updated", change("u1", done("a", "2026-11-10T10:00:00Z")));
  expect(applied.old).toMatchObject({ id: "a", status: "open" });
  expect(user.item("a")).toBeUndefined();
  expect(user.history()).toEqual([]);
  expect(changes[0]!.kinds).toEqual(["items"]);
  expect(store.apply("item.updated", change("u1", done("zz", "2026-11-10T10:00:00Z"))).change).toBe(
    undefined,
  );
});

test("history is ordered like the hub orders it, to the microsecond", () => {
  const { store, user } = loaded();
  store.apply("item.updated", change("u1", done("m1", "2026-11-10T10:00:00.123455Z")));
  store.apply("item.updated", change("u1", done("m2", "2026-11-10T10:00:00.123456Z")));
  store.apply("item.updated", change("u1", done("A", "2026-11-10T10:00:00.1234Z")));
  store.apply("item.updated", change("u1", done("b", "2026-11-10T10:00:00.123400Z")));
  store.apply("item.updated", change("u1", done("old", "2026-11-09T10:00:00Z")));
  store.apply("item.updated", change("u1", done("new", "2026-11-10T11:00:00+00:00")));
  expect(user.history().map((item) => item.id)).toEqual(["new", "m2", "m1", "b", "A", "old"]);
});

test("history parts follow the open items and carry the cursor", () => {
  const { store } = cache();
  store.apply("user.sync", sync("u1", { index: 0, count: 3 }, { items: [wireItem({ id: "a" })] }));
  store.apply(
    "user.sync",
    sync("u1", { index: 1, count: 3 }, { history: [done("h1", "2026-11-10T10:00:00Z")] }),
  );
  store.apply(
    "user.sync",
    sync(
      "u1",
      { index: 2, count: 3 },
      {
        history: [done("h2", "2026-11-09T10:00:00Z")],
        history_cursor: "cur",
      },
    ),
  );
  const user = store.cache.user("u1")!;
  expect(user.history().map((item) => item.id)).toEqual(["h1", "h2"]);
  expect(user.historyCursor).toBe("cur");
  expect(user.items().map((item) => item.id)).toEqual(["a"]);
});

test("an old completed item that changes is taken into the history", () => {
  const { store, user, changes } = loaded({ history: [done("h1", "2026-11-10T10:00:00Z")] });
  const applied = store.apply(
    "item.updated",
    change("u1", done("ancient", "2020-01-01T00:00:00Z")),
  );
  expect(applied.old).toBeUndefined();
  expect(user.history().map((item) => item.id)).toEqual(["h1", "ancient"]);
  expect(changes[0]!.kinds).toEqual(["history"]);
});

test("a completed item that changes keeps its place", () => {
  const { store, user } = loaded({
    history: [done("h1", "2026-11-10T10:00:00Z"), done("h2", "2026-11-09T10:00:00Z")],
  });
  const before = user.item("h2");
  const applied = store.apply(
    "item.updated",
    change("u1", done("h2", "2026-11-09T10:00:00Z", { title: "renamed" })),
  );
  expect(applied.old).toBe(before);
  expect(user.history().map((item) => item.title)).toEqual(["Hand in the report", "renamed"]);
});

test("item(id) finds a completed item, and a deleted one is gone from both", () => {
  const { store, user, changes } = loaded({ history: [done("h1", "2026-11-10T10:00:00Z")] });
  expect(user.item("h1")!.status).toBe("done");
  const gone = store.apply("item.deleted", change("u1", { id: "h1" }));
  expect(gone.old).toMatchObject({ id: "h1" });
  expect(user.item("h1")).toBeUndefined();
  expect(changes[0]!.kinds).toEqual(["history"]);
  store.apply("item.deleted", change("u1", { id: "a" }));
  expect(user.items().map((item) => item.id)).toEqual(["b"]);
  expect(store.apply("item.deleted", change("u1", { id: "nope" })).change).toBeUndefined();
});

test("lists follow their position", () => {
  const { store, user } = loaded();
  store.apply("list.created", change("u1", wireList({ id: "l3", position: 2, is_inbox: false })));
  store.apply("list.created", change("u1", wireList({ id: "l2", position: 1, is_inbox: false })));
  expect(user.lists().map((list) => list.id)).toEqual(["l1", "l2", "l3"]);
  store.apply("list.updated", change("u1", wireList({ id: "l3", position: 1, is_inbox: false })));
  store.apply("list.updated", change("u1", wireList({ id: "l2", position: 2, is_inbox: false })));
  expect(user.lists().map((list) => list.id)).toEqual(["l1", "l3", "l2"]);
  expect(user.list("l3")!.position).toBe(1);
});

test("a deleted list goes and old carries it", () => {
  const { store, user, changes } = loaded();
  const list = user.list("l1");
  const applied = store.apply("list.deleted", change("u1", { id: "l1" }, "n9"));
  expect(applied.old).toBe(list);
  expect(user.lists()).toEqual([]);
  expect(changes).toEqual([
    { userId: "u1", kinds: ["lists"], cause: { name: "list.deleted", nonce: "n9", id: "l1" } },
  ]);
});

test("a plan replaces the one with its priority", () => {
  const { store, user, changes } = loaded();
  const applied = store.apply(
    "plan.updated",
    change("u1", wirePlan({ priority: "normal", before: ["3h"], is_default: false })),
  );
  expect(applied.old).toMatchObject({ priority: "normal", before: [86_400_000] });
  expect(user.plans().map((plan) => plan.priority)).toEqual(["low", "normal", "high"]);
  expect(user.plan("normal")!.before).toEqual([10_800_000]);
  expect(changes[0]).toEqual({
    userId: "u1",
    kinds: ["plans"],
    cause: { name: "plan.updated", nonce: null, id: "normal" },
  });
});

test("the profile is replaced by user.updated", () => {
  const { store, user, changes } = loaded();
  const before = user.profile;
  const applied = store.apply("user.updated", change("u1", wireUser({ display_name: "Mia K." })));
  expect(applied.old).toBe(before);
  expect(user.profile!.displayName).toBe("Mia K.");
  expect(changes[0]!.kinds).toEqual(["profile"]);
});

test("subitem and reminder events leave the cache alone but know the old object", () => {
  const subitem = {
    id: "s1",
    item_id: "a",
    title: "x",
    done: false,
    position: 0,
    created_at: "2026-11-01T10:00:00Z",
    updated_at: "2026-11-01T10:00:00Z",
  };
  const { store, user, changes } = loaded({ items: [wireItem({ id: "a", subitems: [subitem] })] });
  const item = user.item("a")!;
  const applied = store.apply("subitem.updated", change("u1", { ...subitem, done: true }));
  expect(applied.change).toBeUndefined();
  expect(applied.old).toBe(item.subitems[0]);
  expect(store.apply("subitem.deleted", change("u1", { id: "s1", item_id: "a" })).old).toBe(
    item.subitems[0],
  );
  expect(store.apply("reminder.deleted", change("u1", { id: "r1", item_id: "a" })).old).toBe(
    undefined,
  );
  expect(store.apply("delivery.created", change("u1", { id: "d1" })).change).toBeUndefined();
  expect(user.item("a")).toBe(item);
  expect(changes).toEqual([]);
});

test("user.gone drops the user, visible or not", () => {
  const { store, changes } = loaded();
  store.apply("user.gone", { user_id: "u1" });
  expect(store.cache.user("u1")).toBeUndefined();
  expect(store.cache.users()).toEqual([]);
  expect(changes).toEqual([{ userId: "u1", kinds, cause: undefined }]);

  const fresh = cache(["u2"]);
  fresh.store.apply("user.sync", sync("u2", { index: 0, count: 2 }, { items: [] }));
  fresh.store.apply("user.gone", { user_id: "u2" });
  fresh.store.apply("user.sync", sync("u2", { index: 1, count: 2 }, { items: [] }));
  expect(fresh.store.cache.user("u2")).toBeUndefined();
});

test("a new session keeps the old state readable until the new one is complete", () => {
  const { store, user, changes } = loaded();
  store.begin(["u1"], all, "u1");
  store.apply("user.sync", sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "x" })] }));
  expect(store.cache.user("u1")).toBe(user);
  expect(user.items().map((item) => item.id)).toEqual(["a", "b"]);
  expect(changes).toEqual([]);
  store.apply("user.sync", sync("u1", { index: 1, count: 2 }, { items: [wireItem({ id: "y" })] }));
  expect(store.cache.user("u1")).toBe(user);
  expect(user.items().map((item) => item.id)).toEqual(["x", "y"]);
  expect(user.lists()).toEqual([]);
  expect(changes).toHaveLength(1);
});

test("a part 0 that arrives again starts the user over", () => {
  const { store } = cache();
  store.apply("user.sync", sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "a" })] }));
  store.apply("user.sync", sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "a" })] }));
  store.apply("user.sync", sync("u1", { index: 1, count: 2 }, { items: [wireItem({ id: "b" })] }));
  expect(
    store.cache
      .user("u1")!
      .items()
      .map((item) => item.id),
  ).toEqual(["a", "b"]);
});

test("a user the new session no longer names is removed", () => {
  const { store, changes } = loaded();
  store.begin(["u2"], all, "u2");
  expect(store.cache.user("u1")).toBeUndefined();
  expect(changes).toEqual([{ userId: "u1", kinds, cause: undefined }]);
});

test("user() without an id is the caller's own user", () => {
  const { store, user } = loaded();
  expect(store.cache.user()).toBe(user);
  const connection = cache(["u1"], all, null);
  connection.store.apply("user.sync", sync("u1", { index: 0, count: 1 }, { items: [] }));
  expect(connection.store.cache.user()).toBeUndefined();
  expect(connection.store.cache.user("u1")).toBeDefined();
});

test("a listener that throws does not stop the others, and unsubscribing works", () => {
  const { store } = loaded();
  const errors = vi.spyOn(console, "error").mockImplementation(() => {});
  const seen: string[] = [];
  store.cache.subscribe(() => {
    throw new Error("boom");
  });
  const off = store.cache.subscribe((seenChange) => seen.push(seenChange.kinds.join()));
  store.apply("item.updated", change("u1", wireItem({ id: "a" })));
  off();
  store.apply("item.updated", change("u1", wireItem({ id: "a" })));
  expect(seen).toEqual(["items"]);
  expect(errors).toHaveBeenCalledTimes(2);
  errors.mockRestore();
});

test("an event for a user without state changes nothing", () => {
  const { store, changes } = loaded();
  const applied = store.apply("item.created", change("u9", wireItem({ id: "z" })));
  expect(applied.change).toBeUndefined();
  expect((applied.event as { userId: string }).userId).toBe("u9");
  expect(changes).toEqual([]);
});

test("objects from the cache are what the SDK promises: converted and of the right kind", () => {
  const { user } = loaded();
  const list: List = user.lists()[0]!;
  expect(list.isInbox).toBe(true);
  expect(user.items()[0]!.createdAt).toBeInstanceOf(Date);
});

test("a completion time the SDK cannot read does not keep the item out", () => {
  const { store, user } = loaded();
  store.apply("item.updated", change("u1", done("a", "whenever")));
  expect(user.history().map((item) => item.id)).toEqual(["a"]);
});
