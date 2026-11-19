import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { Client, GatewayError, type Intent, type Item } from "../src/index.js";
import { fakeFetch } from "./fake-fetch.js";
import { FakeGateway, ready, settle } from "./fake-gateway.js";
import { change, sync, wireDelivery, wireItem, wireList, wireUser } from "./fixtures.js";

const info = {
  status: 200,
  body: { name: "hub", version: "1", gateway_url: "wss://hub.test/api/v1/gateway" },
};

function setup(options: { intents?: Intent[] } = { intents: ["users", "lists", "items"] }) {
  const { fetch, calls } = fakeFetch([info]);
  const fake = new FakeGateway();
  const client = new Client(
    { url: "https://hub.test", token: "plsr_pat_x", fetch, ...options },
    { createSocket: fake.createSocket, random: () => 0.5 },
  );
  return { client, fake, calls };
}

async function connected(parts: Record<string, unknown> = {}) {
  const made = setup({ intents: ["users", "lists", "items", "history", "deliveries"] });
  const connecting = made.client.connect();
  const socket = await made.fake.opened();
  socket.hello();
  ready(socket);
  socket.dispatch(
    "user.sync",
    sync(
      "u1",
      { index: 0, count: 1 },
      {
        user: wireUser(),
        lists: [wireList()],
        items: [wireItem({ id: "a" })],
        history: [],
        history_cursor: null,
        ...parts,
      },
    ),
    2,
  );
  await connecting;
  return { ...made, socket };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test("connect returns once every user of the session is complete", async () => {
  const { client, fake, calls } = setup({ intents: ["items"] });
  expect(client.status).toBe("idle");
  let done = false;
  const connecting = client.connect().then(() => (done = true));
  expect(client.connect()).toBe(client.connect());
  expect(client.status).toBe("connecting");
  const socket = await fake.opened();
  expect(calls[0]!.url).toBe("https://hub.test/api/v1/info");
  socket.hello();
  expect(socket.sent[0]).toEqual({ op: 2, d: { token: "plsr_pat_x", intents: ["items"] } });
  ready(socket, { users: ["u1", "u2"] });
  socket.dispatch("user.sync", sync("u1", { index: 0, count: 1 }, { items: [] }), 2);
  await settle();
  expect(done).toBe(false);
  socket.dispatch("user.gone", { user_id: "u2" }, 3);
  await connecting;
  expect(client.status).toBe("connected");
  expect(client.principal).toEqual({ kind: "personal_token", id: "p1", userId: "u1" });
  expect(client.cache.users()).toHaveLength(1);
});

test("a session without state is ready with READY alone", async () => {
  const { client, fake } = setup({ intents: ["deliveries"] });
  const connecting = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket, {
    users: [],
    principal: {
      kind: "connection",
      id: "c1",
      connection: { id: "c1", name: "Bot", provider: "discord" },
    },
  });
  await connecting;
  expect(client.principal).toEqual({
    kind: "connection",
    id: "c1",
    connection: { id: "c1", name: "Bot", provider: "discord" },
  });
  expect(client.cache.user()).toBeUndefined();
});

test("the cache is already changed when a handler runs, and the cache listener came first", async () => {
  const { client, socket } = await connected();
  const order: string[] = [];
  client.cache.subscribe(() => order.push("cache"));
  client.on("item.updated", (event) => {
    order.push("handler");
    expect(client.cache.user()!.item("a")).toBe(event.data);
  });
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a", title: "new" })), 3);
  expect(order).toEqual(["cache", "handler"]);
});

test("an event carries the object from before the change, in SDK names and types", async () => {
  const { client, socket } = await connected();
  const seen: { old: Item | undefined; data: Item; userId: string; nonce: string | null }[] = [];
  client.on("item.updated", (event) => seen.push(event));
  socket.dispatch(
    "item.updated",
    change("u1", wireItem({ id: "a", title: "new", due_at: "2026-11-13T18:00:00Z" }), "n1"),
    3,
  );
  expect(seen[0]!.old!.title).toBe("Hand in the report");
  expect(seen[0]!.data.title).toBe("new");
  expect(seen[0]!.data.dueAt).toBeInstanceOf(Date);
  expect(seen[0]!.userId).toBe("u1");
  expect(seen[0]!.nonce).toBe("n1");
  expect(seen[0]).toMatchObject({ actor: { kind: "session", id: "c1" } });

  const created: unknown[] = [];
  client.on("item.created", (event) => created.push(event.old));
  socket.dispatch("item.created", change("u1", wireItem({ id: "b" })), 4);
  expect(created).toEqual([undefined]);
});

test("ready comes after every identify, user.ready after every complete sync", async () => {
  const { client, fake } = setup({ intents: ["items"] });
  const seen: string[] = [];
  client.on("user.ready", (event) => seen.push(`user.ready ${event.userId}`));
  client.on("ready", (event) => seen.push(`ready ${event.users.join()} ${event.principal.id}`));
  client.on("resumed", () => seen.push("resumed"));
  const connecting = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket);
  socket.dispatch("user.sync", sync("u1", { index: 0, count: 2 }, { items: [] }), 2);
  expect(seen).toEqual([]);
  socket.dispatch("user.sync", sync("u1", { index: 1, count: 2 }, { items: [] }), 3);
  await connecting;
  expect(seen).toEqual(["user.ready u1", "ready u1 p1"]);

  socket.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const second = await fake.opened();
  second.hello();
  second.dispatch("RESUMED", {}, 4);
  expect(seen.at(-1)).toBe("resumed");
  expect(client.status).toBe("connected");

  second.op(9, false);
  ready(second, { session: "s2" });
  second.dispatch("user.sync", sync("u1", { index: 0, count: 1 }, { items: [] }), 2);
  expect(seen.slice(-2)).toEqual(["user.ready u1", "ready u1 p1"]);
});

test("user.sync is reported part by part", async () => {
  const { client, fake } = setup({ intents: ["items", "history"] });
  const parts: unknown[] = [];
  client.on("user.sync", (event) =>
    parts.push([
      event.chunk.index,
      event.items?.length,
      event.history?.length,
      event.historyCursor,
    ]),
  );
  const connecting = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket);
  socket.dispatch("user.sync", sync("u1", { index: 0, count: 2 }, { items: [wireItem()] }), 2);
  socket.dispatch(
    "user.sync",
    sync("u1", { index: 1, count: 2 }, { history: [], history_cursor: "cur" }),
    3,
  );
  await connecting;
  expect(parts).toEqual([
    [0, 1, undefined, undefined],
    [1, undefined, 0, "cur"],
  ]);
});

test("an unknown event is passed over", async () => {
  const { client, socket } = await connected();
  const errors: unknown[] = [];
  client.on("error", (event) => errors.push(event));
  socket.dispatch("item.exploded", change("u1", { id: "a" }), 3);
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a", title: "still works" })), 4);
  expect(client.cache.user()!.item("a")!.title).toBe("still works");
  expect(errors).toEqual([]);
});

test("a handler that throws and one that rejects both end up at error, and the rest still run", async () => {
  const { client, socket } = await connected();
  const errors: { error: unknown; event: string }[] = [];
  const ran: string[] = [];
  client.on("error", (event) => errors.push(event));
  client.on("item.updated", () => {
    throw new Error("thrown");
  });
  client.on("item.updated", async () => {
    throw new Error("rejected");
  });
  client.on("item.updated", () => {
    ran.push("third");
  });
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 3);
  await settle();
  expect(ran).toEqual(["third"]);
  expect(errors.map((seen) => [(seen.error as Error).message, seen.event])).toEqual([
    ["thrown", "item.updated"],
    ["rejected", "item.updated"],
  ]);
  expect(client.status).toBe("connected");
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 4);
  expect(ran).toEqual(["third", "third"]);
});

test("without an error handler, or when it throws itself, the failure is written to the console", async () => {
  const { client, socket } = await connected();
  const written = vi.spyOn(console, "error").mockImplementation(() => {});
  client.on("item.updated", () => {
    throw new Error("thrown");
  });
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 3);
  expect(written).toHaveBeenCalledTimes(1);
  client.on("error", () => {
    throw new Error("the error handler is broken too");
  });
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 4);
  expect(written).toHaveBeenCalledTimes(2);
});

test("a drop between two sync parts is finished by the resume", async () => {
  const { client, fake } = setup({ intents: ["items"] });
  const connecting = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket);
  socket.dispatch(
    "user.sync",
    sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "a" })] }),
    2,
  );
  socket.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const second = await fake.opened();
  second.hello();
  expect(second.sent[0]).toMatchObject({ op: 6, d: { seq: 2 } });
  second.dispatch(
    "user.sync",
    sync("u1", { index: 1, count: 2 }, { items: [wireItem({ id: "b" })] }),
    3,
  );
  second.dispatch("RESUMED", {}, 4);
  await connecting;
  expect(
    client.cache
      .user()!
      .items()
      .map((item) => item.id),
  ).toEqual(["a", "b"]);
});

test("a user the new session no longer names is reported gone before the client is ready again", async () => {
  const { client, socket, fake } = await connected();
  const seen: string[] = [];
  client.on("user.gone", (event) => seen.push(`gone ${event.userId}`));
  client.on("ready", (event) => seen.push(`ready ${event.users.join()}`));
  socket.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const second = await fake.opened();
  second.hello();
  second.op(9, false);
  ready(second, { session: "s2", users: [] });
  await settle();
  expect(seen).toEqual(["gone u1", "ready "]);
  expect(client.cache.user("u1")).toBeUndefined();
});

test("an invalid session between two sync parts starts over and keeps the old state readable", async () => {
  const { client, socket, fake } = await connected();
  socket.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const second = await fake.opened();
  second.hello();
  second.op(9, false);
  ready(second, { session: "s2" });
  second.dispatch(
    "user.sync",
    sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "x" })] }),
    2,
  );
  expect(
    client.cache
      .user()!
      .items()
      .map((item) => item.id),
  ).toEqual(["a"]);
  second.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const third = await fake.opened();
  third.hello();
  third.op(9, false);
  ready(third, { session: "s3" });
  expect(
    client.cache
      .user()!
      .items()
      .map((item) => item.id),
  ).toEqual(["a"]);
  third.dispatch(
    "user.sync",
    sync("u1", { index: 0, count: 2 }, { items: [wireItem({ id: "x" })] }),
    2,
  );
  third.dispatch(
    "user.sync",
    sync("u1", { index: 1, count: 2 }, { items: [wireItem({ id: "y" })] }),
    3,
  );
  expect(
    client.cache
      .user()!
      .items()
      .map((item) => item.id),
  ).toEqual(["x", "y"]);
});

test("connect rejects when the login is refused for good", async () => {
  const { client, fake } = setup();
  const closed: unknown[] = [];
  client.on("closed", (event) => closed.push(event));
  const connecting = client.connect();
  const refused = expect(connecting).rejects.toMatchObject({ code: 4004 });
  const socket = await fake.opened();
  socket.hello();
  socket.drop(4004, "authentication failed");
  await refused;
  await expect(connecting).rejects.toBeInstanceOf(GatewayError);
  expect(closed).toEqual([{ code: 4004, reason: "authentication failed" }]);
  expect(client.status).toBe("closed");
});

test("losing access later ends the client with closed", async () => {
  const { client, socket, fake } = await connected();
  const seen: string[] = [];
  client.on("closed", (event) => seen.push(`closed ${event.code}`));
  client.on("disconnected", () => seen.push("disconnected"));
  socket.drop(4014, "intent not allowed");
  await vi.advanceTimersByTimeAsync(300_000);
  expect(seen).toEqual(["closed 4014"]);
  expect(client.status).toBe("closed");
  expect(fake.sockets).toHaveLength(1);
  expect(client.cache.user()!.items()).toHaveLength(1);
});

test("connect without intents is refused", async () => {
  const { client, fake } = setup({});
  await expect(client.connect()).rejects.toBeInstanceOf(TypeError);
  expect(fake.sockets).toEqual([]);
  expect(client.status).toBe("idle");
});

test("a drop is reported and the client reconnects", async () => {
  const { client, socket, fake } = await connected();
  const seen: unknown[] = [];
  client.on("disconnected", (event) => seen.push(event));
  socket.drop(1012, "restarting");
  expect(seen).toEqual([{ code: 1012, reason: "restarting" }]);
  expect(client.status).toBe("reconnecting");
  await vi.advanceTimersByTimeAsync(1000);
  expect(fake.sockets).toHaveLength(2);
});

test("close ends everything and connect works again afterwards", async () => {
  const { client, socket, fake } = await connected();
  client.close();
  expect(socket.closedWith).toBe(1000);
  expect(client.status).toBe("idle");
  expect(client.cache.user()!.items()).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets).toHaveLength(1);

  const again = client.connect();
  const second = await fake.opened();
  second.hello();
  expect(second.sent[0]!.op).toBe(2);
  ready(second, { users: [] });
  await again;
  expect(client.status).toBe("connected");
  expect(client.cache.user()).toBeUndefined();
});

test("close while connecting rejects connect", async () => {
  const { client, fake } = setup();
  const connecting = client.connect();
  const refused = expect(connecting).rejects.toBeInstanceOf(GatewayError);
  const socket = await fake.opened();
  socket.hello();
  client.close();
  await refused;
  socket.drop();
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets).toHaveLength(1);
});

test("a delivery is reported every time the hub sends it, and nothing is confirmed", async () => {
  const { client, socket, calls } = await connected();
  const attempts: number[] = [];
  client.on("delivery.created", (event) => attempts.push(event.data.attempt));
  socket.dispatch("delivery.created", change("u1", wireDelivery({ attempt: 1 })), null);
  socket.dispatch("delivery.created", change("u1", wireDelivery({ attempt: 2 })), null);
  await settle();
  expect(attempts).toEqual([1, 2]);
  expect(calls).toHaveLength(1);
});

test("once fires a single time and the returned function unsubscribes", async () => {
  const { client, socket } = await connected();
  const seen: string[] = [];
  client.once("item.updated", () => seen.push("once"));
  const off = client.on("item.updated", () => seen.push("on"));
  const never = client.once("item.updated", () => seen.push("never"));
  never();
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 3);
  off();
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a" })), 4);
  expect(seen).toEqual(["once", "on"]);
});

test("coming back online or into view reconnects at once", async () => {
  const listeners = new Map<string, () => void>();
  vi.stubGlobal("addEventListener", (name: string, listener: () => void) =>
    listeners.set(name, listener),
  );
  vi.stubGlobal("removeEventListener", (name: string) => listeners.delete(name));
  const { client, socket, fake } = await connected();
  socket.drop();
  listeners.get("online")!();
  await settle();
  expect(fake.sockets).toHaveLength(2);
  client.close();
  expect(listeners.size).toBe(0);
});

test("the address of the gateway cannot be fetched: connect rejects with the reason", async () => {
  const { fetch } = fakeFetch([new TypeError("fetch failed")]);
  const client = new Client({ url: "https://hub.test", token: "t", fetch, intents: ["items"] });
  const error = await client.connect().catch((e: unknown) => e);
  expect(error).toBeInstanceOf(GatewayError);
  expect(((error as Error).cause as Error).name).toBe("NetworkError");
  expect(client.status).toBe("closed");
});

test("close from inside a disconnected handler ends the client", async () => {
  const { client, socket, fake } = await connected();
  client.on("disconnected", () => client.close());
  socket.drop();
  await vi.advanceTimersByTimeAsync(300_000);
  expect(client.status).toBe("idle");
  expect(fake.sockets).toHaveLength(1);
});

test("coming back into view reconnects at once, staying hidden does not", async () => {
  const listeners = new Map<string, () => void>();
  const document = {
    visibilityState: "hidden",
    addEventListener: (name: string, listener: () => void) => listeners.set(name, listener),
    removeEventListener: (name: string) => listeners.delete(name),
  };
  vi.stubGlobal("addEventListener", () => {});
  vi.stubGlobal("removeEventListener", () => {});
  vi.stubGlobal("document", document);
  const { client, socket, fake } = await connected();
  socket.drop();
  listeners.get("visibilitychange")!();
  await settle();
  expect(fake.sockets).toHaveLength(1);
  document.visibilityState = "visible";
  listeners.get("visibilitychange")!();
  await settle();
  expect(fake.sockets).toHaveLength(2);
  client.close();
  expect(listeners.size).toBe(0);
});

test("an event the SDK cannot take in goes to error and the next one still arrives", async () => {
  const { client, socket } = await connected();
  const errors: string[] = [];
  client.on("error", (event) => errors.push(event.event));
  socket.dispatch("user.sync", { user_id: "u1" }, 3);
  expect(errors).toEqual(["user.sync"]);
  socket.dispatch(
    "item.updated",
    change("u1", wireItem({ id: "a", data: null, subitems: null })),
    4,
  );
  socket.dispatch("item.updated", change("u1", wireItem({ id: "a", title: "fine" })), 5);
  expect(client.cache.user()!.item("a")!.title).toBe("fine");
  expect(client.status).toBe("connected");
});

test("close inside ready and connect inside closed both do what they say", async () => {
  const { client, fake } = setup({ intents: ["items"] });
  client.once("ready", () => client.close());
  const first = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket, { users: [] });
  await first;
  expect(client.status).toBe("idle");
  expect(socket.closedWith).toBe(1000);

  let again: Promise<void> | undefined;
  client.once("closed", () => {
    again = client.connect();
  });
  const second = client.connect().catch(() => "rejected");
  const refused = await fake.opened();
  refused.hello();
  refused.drop(4004, "no");
  expect(await second).toBe("rejected");
  const third = await fake.opened();
  expect(third).not.toBe(refused);
  third.hello();
  ready(third, { users: [] });
  await again;
  expect(client.status).toBe("connected");
});
