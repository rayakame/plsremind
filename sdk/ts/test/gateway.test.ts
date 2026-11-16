import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { GatewayError } from "../src/index.js";
import { Gateway } from "../src/gateway.js";
import { FakeGateway, ready, settle } from "./fake-gateway.js";

interface Seen {
  dispatches: { name: string; data: unknown }[];
  disconnected: { code: number | undefined; reason: string }[];
  closed: GatewayError[];
  status: string[];
}

function start(
  options: {
    token?: string | undefined;
    intents?: string[];
    url?: () => Promise<string>;
    random?: number;
  } = {},
) {
  const fake = new FakeGateway();
  const seen: Seen = { dispatches: [], disconnected: [], closed: [], status: [] };
  const gateway = new Gateway({
    url: options.url ?? (async () => "wss://hub.test/api/v1/gateway"),
    token: "token" in options ? options.token : "plsr_pat_x",
    intents: options.intents ?? ["items"],
    createSocket: fake.createSocket,
    random: () => options.random ?? 0.5,
    onDispatch: (name, data) => seen.dispatches.push({ name, data }),
    onDisconnected: (code, reason) => seen.disconnected.push({ code, reason }),
    onClosed: (error) => seen.closed.push(error),
    onStatus: (status) => seen.status.push(status),
  });
  gateway.start();
  return { gateway, fake, seen };
}

async function established(options: Parameters<typeof start>[0] = {}) {
  const started = start(options);
  const socket = await started.fake.opened();
  socket.hello();
  ready(socket, { users: [] });
  socket.dispatch("item.created", { user_id: "u1" }, 2);
  return started;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

test("identifies after hello and hands every dispatch on", async () => {
  const { fake, seen } = start();
  const socket = await fake.opened();
  expect(socket.url).toBe("wss://hub.test/api/v1/gateway");
  expect(socket.sent).toEqual([]);
  socket.hello();
  expect(socket.sent).toEqual([{ op: 2, d: { token: "plsr_pat_x", intents: ["items"] } }]);
  ready(socket, { users: [] });
  socket.dispatch("item.created", { user_id: "u1" }, 2);
  expect(seen.dispatches.map((dispatch) => dispatch.name)).toEqual(["READY", "item.created"]);
  expect(seen.dispatches[1]!.data).toEqual({ user_id: "u1" });
  expect(seen.status).toEqual(["connecting", "connected"]);
});

test("a session leaves the token out of identify", async () => {
  const { fake } = start({ token: undefined });
  const socket = await fake.opened();
  socket.hello();
  expect(socket.sent).toEqual([{ op: 2, d: { intents: ["items"] } }]);
});

test("beats once per interval with the last sequence number, the first after a random part", async () => {
  const { fake } = await established();
  const socket = fake.current;
  await vi.advanceTimersByTimeAsync(22_499);
  expect(socket.beats()).toEqual([]);
  await vi.advanceTimersByTimeAsync(1);
  expect(socket.beats()).toEqual([{ op: 1, d: 2 }]);
  socket.op(11);
  socket.dispatch("item.updated", {}, 3);
  await vi.advanceTimersByTimeAsync(45_000);
  expect(socket.beats()).toEqual([
    { op: 1, d: 2 },
    { op: 1, d: 3 },
  ]);
});

test("a heartbeat without an answer ends the connection and resumes", async () => {
  const { fake, seen } = await established();
  const first = fake.current;
  await vi.advanceTimersByTimeAsync(22_500);
  await vi.advanceTimersByTimeAsync(45_000);
  expect(first.closedWith).toBe(4000);
  expect(seen.disconnected).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(1000);
  const second = await fake.opened();
  expect(second).not.toBe(first);
  second.hello();
  expect(second.sent).toEqual([{ op: 6, d: { token: "plsr_pat_x", session_id: "s1", seq: 2 } }]);
});

test("a dropped connection is resumed after one second, doubling up to a minute", async () => {
  const { fake, seen } = await established();
  const waits: number[] = [];
  for (let i = 0; i < 9; i++) {
    const before = fake.sockets.length;
    fake.current.drop(1006);
    let waited = 0;
    while (fake.sockets.length === before) {
      await vi.advanceTimersByTimeAsync(250);
      waited += 250;
    }
    waits.push(waited);
    await fake.opened();
  }
  expect(waits).toEqual([750, 1500, 3000, 6000, 12_000, 24_000, 45_000, 45_000, 45_000]);
  expect(seen.status.at(-1)).toBe("reconnecting");
  expect(seen.closed).toEqual([]);
});

test("a successful resume starts the wait over", async () => {
  const { fake } = await established();
  for (let i = 0; i < 3; i++) {
    fake.current.drop();
    await vi.advanceTimersByTimeAsync(10_000);
    await fake.opened();
  }
  fake.current.hello();
  fake.current.dispatch("RESUMED", {}, 3);
  const before = fake.sockets.length;
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(749);
  expect(fake.sockets.length).toBe(before);
  await vi.advanceTimersByTimeAsync(1);
  await settle();
  expect(fake.sockets.length).toBe(before + 1);
});

test("reconnect from the hub is followed at once", async () => {
  const { fake } = await established();
  const first = fake.current;
  first.op(7);
  const second = await fake.opened();
  expect(second).not.toBe(first);
  expect(first.closedWith).toBe(4000);
  second.hello();
  expect(second.sent[0]).toMatchObject({ op: 6, d: { session_id: "s1", seq: 2 } });
});

test("an invalid session identifies again on the same connection", async () => {
  const { fake, seen } = await established();
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const socket = await fake.opened();
  socket.hello();
  socket.op(9, false);
  expect(socket.sent.map((message) => message.op)).toEqual([6, 2]);
  expect(fake.current).toBe(socket);
  ready(socket, { users: [], session: "s2" });
  socket.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const next = await fake.opened();
  next.hello();
  expect(next.sent[0]).toMatchObject({ op: 6, d: { session_id: "s2", seq: 1 } });
});

test.each([4004, 4013, 4014])("close code %i ends the client for good", async (code) => {
  const { fake, seen } = await established();
  const sockets = fake.sockets.length;
  fake.current.drop(code, "no");
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets.length).toBe(sockets);
  expect(seen.closed).toHaveLength(1);
  expect(seen.closed[0]!.code).toBe(code);
});

test("4007 forgets the session and identifies", async () => {
  const { fake } = await established();
  fake.current.drop(4007);
  await vi.advanceTimersByTimeAsync(1000);
  const socket = await fake.opened();
  socket.hello();
  expect(socket.sent[0]!.op).toBe(2);
});

test("4008 waits at least five seconds", async () => {
  const { fake } = await established();
  const before = fake.sockets.length;
  fake.current.drop(4008);
  await vi.advanceTimersByTimeAsync(4999);
  await settle();
  expect(fake.sockets.length).toBe(before);
  await vi.advanceTimersByTimeAsync(1);
  await settle();
  expect(fake.sockets.length).toBe(before + 1);
});

test("delivery events do not move the sequence number", async () => {
  const { fake } = await established();
  fake.current.dispatch("delivery.created", { user_id: "u1" }, null);
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const socket = await fake.opened();
  socket.hello();
  expect(socket.sent[0]).toMatchObject({ op: 6, d: { seq: 2 } });
});

test("stop during the wait opens no further socket", async () => {
  const { gateway, fake } = await established();
  const before = fake.sockets.length;
  fake.current.drop();
  gateway.stop();
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets.length).toBe(before);
});

test("stop closes the socket, ends the heartbeat and ignores what still arrives", async () => {
  const { gateway, fake, seen } = await established();
  const socket = fake.current;
  gateway.stop();
  expect(socket.closedWith).toBe(1000);
  socket.dispatch("item.created", {}, 3);
  socket.drop(1000);
  await vi.advanceTimersByTimeAsync(300_000);
  expect(socket.beats()).toEqual([]);
  expect(seen.dispatches).toHaveLength(2);
  expect(seen.disconnected).toEqual([]);
  expect(fake.sockets).toHaveLength(1);
});

test("stop while the address is still being fetched opens no socket", async () => {
  let release!: (url: string) => void;
  const { gateway, fake, seen } = start({
    url: () => new Promise((resolve) => (release = resolve)),
  });
  gateway.stop();
  release("wss://hub.test/api/v1/gateway");
  await settle();
  expect(fake.sockets).toEqual([]);
  expect(seen.closed).toEqual([]);
});

test("wake skips the wait", async () => {
  const { gateway, fake } = await established();
  const before = fake.sockets.length;
  fake.current.drop();
  gateway.wake();
  await settle();
  expect(fake.sockets.length).toBe(before + 1);
  (await fake.opened()).hello();
  fake.current.dispatch("RESUMED", {}, 3);
  gateway.wake();
  fake.current.op(11);
  await vi.advanceTimersByTimeAsync(40_000);
  expect(fake.sockets.length).toBe(before + 1);
});

test("a first attempt that fails is reported and not repeated", async () => {
  const refused = start();
  (await refused.fake.opened()).drop(1006);
  await vi.advanceTimersByTimeAsync(300_000);
  expect(refused.fake.sockets).toHaveLength(1);
  expect(refused.seen.closed).toHaveLength(1);

  const cause = new Error("no info");
  const unreachable = start({ url: () => Promise.reject(cause) });
  await settle();
  expect(unreachable.fake.sockets).toEqual([]);
  expect(unreachable.seen.closed[0]!.cause).toBe(cause);
});

test("a frame that is not JSON is ignored", async () => {
  const { fake, seen } = await established();
  fake.current.raw("not json");
  fake.current.raw("null");
  fake.current.dispatch("item.updated", {}, 3);
  expect(seen.dispatches).toHaveLength(3);
});

test("after stop a new start identifies from scratch", async () => {
  const { gateway, fake } = await established();
  gateway.stop();
  gateway.start();
  const socket = await fake.opened();
  socket.hello();
  expect(socket.sent[0]!.op).toBe(2);
});

test("stop from inside the disconnected callback really stops", async () => {
  const fake = new FakeGateway();
  const status: string[] = [];
  const gateway: Gateway = new Gateway({
    url: async () => "wss://hub.test/api/v1/gateway",
    token: "t",
    intents: ["items"],
    createSocket: fake.createSocket,
    random: () => 0.5,
    onDispatch: () => {},
    onDisconnected: () => gateway.stop(),
    onClosed: () => {},
    onStatus: (seen) => status.push(seen),
  });
  gateway.start();
  const socket = await fake.opened();
  socket.hello();
  ready(socket, { users: [] });
  socket.drop();
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets).toHaveLength(1);
  expect(status.at(-1)).toBe("reconnecting");

  gateway.start();
  const second = await fake.opened();
  second.hello();
  ready(second, { users: [] });
  second.op(7);
  await vi.advanceTimersByTimeAsync(300_000);
  expect(fake.sockets).toHaveLength(2);
});

test("a socket that never says hello is given up after fifteen seconds", async () => {
  const { fake, seen } = await established();
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const silent = await fake.opened();
  await vi.advanceTimersByTimeAsync(14_999);
  expect(fake.current).toBe(silent);
  await vi.advanceTimersByTimeAsync(1);
  expect(silent.closedWith).toBe(4000);
  expect(seen.disconnected).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(2000);
  await settle();
  expect(fake.current).not.toBe(silent);
  const next = fake.current;
  next.open();
  next.hello();
  next.dispatch("RESUMED", {}, 3);
  await vi.advanceTimersByTimeAsync(20_000);
  expect(fake.current).toBe(next);
  expect(next.closedWith).toBeUndefined();
});

test("a first attempt that never gets a hello fails instead of hanging", async () => {
  const { fake, seen } = start();
  await fake.opened();
  await vi.advanceTimersByTimeAsync(15_000);
  expect(seen.closed).toHaveLength(1);
  expect(fake.sockets).toHaveLength(1);
});

test("wake abandons an attempt that has not got its hello yet", async () => {
  const { gateway, fake } = await established();
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const stuck = await fake.opened();
  gateway.wake();
  await settle();
  expect(stuck.closedWith).toBe(4000);
  expect(fake.current).not.toBe(stuck);
  const live = await fake.opened();
  live.hello();
  gateway.wake();
  await settle();
  expect(fake.current).toBe(live);
});

test("a handshake that stalls after hello is given up as well", async () => {
  const { fake, seen } = await established();
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const stalled = await fake.opened();
  stalled.hello();
  await vi.advanceTimersByTimeAsync(14_000);
  stalled.dispatch("item.updated", {}, 3);
  await vi.advanceTimersByTimeAsync(14_000);
  expect(stalled.closedWith).toBeUndefined();
  await vi.advanceTimersByTimeAsync(1000);
  expect(stalled.closedWith).toBe(4000);
  expect(seen.closed).toEqual([]);
});

test("answered heartbeats do not keep a stalled handshake waiting", async () => {
  const { fake, seen } = await established();
  fake.current.drop();
  await vi.advanceTimersByTimeAsync(1000);
  const stalled = await fake.opened();
  stalled.hello();
  await vi.advanceTimersByTimeAsync(14_000);
  stalled.op(11);
  await vi.advanceTimersByTimeAsync(1000);
  expect(stalled.closedWith).toBe(4000);
  expect(seen.closed).toEqual([]);
});

test("an outage is reported once, however many attempts it takes", async () => {
  const { fake, seen } = await established();
  for (let i = 0; i < 4; i++) {
    fake.current.drop();
    await vi.advanceTimersByTimeAsync(20_000);
    await fake.opened();
  }
  expect(seen.disconnected).toHaveLength(1);
  fake.current.hello();
  fake.current.dispatch("RESUMED", {}, 3);
  fake.current.drop(1012, "restarting");
  expect(seen.disconnected).toEqual([
    { code: 1006, reason: "" },
    { code: 1012, reason: "restarting" },
  ]);
});

test("wake on a live connection asks for a sign of life and drops it if none comes", async () => {
  const { gateway, fake, seen } = await established();
  const socket = fake.current;
  gateway.wake();
  gateway.wake();
  expect(socket.beats()).toEqual([{ op: 1, d: 2 }]);
  socket.op(11);
  await vi.advanceTimersByTimeAsync(10_000);
  expect(socket.closedWith).toBeUndefined();

  gateway.wake();
  await vi.advanceTimersByTimeAsync(4999);
  expect(socket.closedWith).toBeUndefined();
  await vi.advanceTimersByTimeAsync(1);
  expect(socket.closedWith).toBe(4000);
  expect(seen.disconnected).toHaveLength(1);
});
