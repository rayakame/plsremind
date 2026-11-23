import { act, render } from "@testing-library/react";
import { Client, type Intent } from "plsremind";
import { optimistic } from "plsremind/optimistic";
import { memoryLocation } from "wouter/memory-location";
import { FakeGateway, ready, settle } from "../../sdk/ts/test/fake-gateway.js";
import { sync, wireItem, wireList, wirePlan, wireUser } from "../../sdk/ts/test/fixtures.js";
import { App } from "../src/app.js";
import { setLocale } from "../src/i18n/index.js";

export { change, wireItem, wireList, wirePlan, wireUser } from "../../sdk/ts/test/fixtures.js";
export { deferred } from "../../sdk/ts/test/connected.js";
export { settle };

type Wire = Record<string, unknown>;

export interface Call {
  method: string;
  path: string;
  body: unknown;
  headers: Record<string, string>;
}

export interface Answer {
  status: number;
  body?: unknown;
}

type Answering = Answer | ((call: Call) => Answer | Promise<Answer>);

export interface HubOptions {
  /** The signed-in user's profile; null for nobody signed in. */
  user?: Wire | null;
  info?: Wire;
  /** Parts of the first sync that replace the defaults. */
  sync?: Wire;
}

const intents: Intent[] = ["users", "lists", "items", "plans", "history"];

/** A hub made of a routed fake fetch and a fake gateway. */
export function fakeHub(options: HubOptions = {}) {
  const calls: Call[] = [];
  const routes: { method: string; path: string | RegExp; answer: Answering }[] = [];
  const gateway = new FakeGateway();
  let user = options.user === undefined ? wireUser({ locale: "en" }) : options.user;
  let seq = 2;

  /** Answers a request from now on. Later routes win over earlier ones. */
  const route = (method: string, path: string | RegExp, answer: Answering) => {
    routes.unshift({ method, path, answer });
  };
  route("GET", "/info", {
    status: 200,
    body: {
      name: "plsremind",
      version: "test",
      gateway_url: "ws://hub.test/api/v1/gateway",
      setup_required: false,
      registration: "open",
      login_methods: [
        { key: "password", type: "password", name: "Password", login: true, link: false },
      ],
      ...options.info,
    },
  });
  route("GET", "/users/@me", () =>
    user ? { status: 200, body: user } : { status: 401, body: { error: { code: "unauthorized" } } },
  );

  route("GET", /^\/users\/@me\/items\/[^/]+\/schedule/, { status: 200, body: [] });
  route("GET", "/users/@me/reminder-log", { status: 200, body: { data: [], next_cursor: null } });

  const fetch = (async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = new URL(String(input));
    const headers: Record<string, string> = {};
    new Headers(init.headers).forEach((value, key) => (headers[key] = value));
    const call: Call = {
      method: init.method ?? "GET",
      path: url.pathname.replace(/^\/api\/v1/, "") + url.search,
      body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
      headers,
    };
    calls.push(call);
    init.signal?.throwIfAborted();
    const found = routes.find(
      (candidate) =>
        candidate.method === call.method &&
        (typeof candidate.path === "string"
          ? candidate.path === call.path.split("?")[0]
          : candidate.path.test(call.path)),
    );
    const answer = found
      ? typeof found.answer === "function"
        ? await found.answer(call)
        : found.answer
      : {
          status: 500,
          body: { error: { code: "no_route", message: `${call.method} ${call.path}` } },
        };
    return new Response(answer.status === 204 ? null : JSON.stringify(answer.body ?? null), {
      status: answer.status,
    });
  }) as typeof globalThis.fetch;

  const make = () =>
    new Client(
      { url: "http://hub.test", auth: "session", intents, fetch },
      { createSocket: gateway.createSocket, random: () => 0.5 },
    );

  /** Lets the newest socket log in and sends the first state. */
  async function connect(parts: Wire = {}) {
    const socket = await gateway.opened();
    socket.hello();
    ready(socket, { intents, principal: { kind: "session", id: "c1", user_id: "u1" } });
    socket.dispatch(
      "user.sync",
      sync(
        "u1",
        { index: 0, count: 1 },
        {
          user,
          lists: [wireList(), wireList({ id: "l2", name: "Work", position: 1, is_inbox: false })],
          plans: [wirePlan({ priority: "low" }), wirePlan(), wirePlan({ priority: "high" })],
          items: [wireItem({ id: "a" }), wireItem({ id: "b", title: "Call Mia" })],
          history: [],
          history_cursor: null,
          ...options.sync,
          ...parts,
        },
      ),
      2,
    );
    await settle();
  }

  return {
    calls,
    route,
    gateway,
    make,
    connect,
    setUser: (next: Wire | null) => {
      user = next;
    },
    /** An event from the hub, as the gateway would send it. */
    send: (name: string, data: unknown) => act(() => gateway.current.dispatch(name, data, ++seq)),
    /** The calls made so far with this method and path. */
    sent: (method: string, path: string | RegExp) =>
      calls.filter(
        (call) =>
          call.method === method &&
          (typeof path === "string" ? call.path.split("?")[0] === path : path.test(call.path)),
      ),
  };
}

export interface AppOptions extends HubOptions {
  path?: string;
  /** Leave the gateway unanswered, to drive the connection by hand. */
  hold?: boolean;
}

/** The whole app against a fake hub, signed in and synced unless told otherwise. */
export async function renderApp(options: AppOptions = {}) {
  setLocale("en");
  const hub = fakeHub(options);
  const location = memoryLocation({ path: options.path ?? "/", record: true });
  const view = render(<App make={hub.make} location={location} />);
  await act(settle);
  const signedIn = options.user !== null && options.info?.setup_required !== true;
  if (signedIn && !options.hold) await act(() => hub.connect());
  return { ...hub, ...view, location, path: () => location.history.at(-1)! };
}

/** A connected client and its view against a fake hub, without the app around it. */
export async function connectedHub(options: HubOptions = {}) {
  const hub = fakeHub(options);
  const client = hub.make();
  const connecting = client.connect();
  await hub.connect();
  await connecting;
  const view = optimistic(client);
  return { ...hub, client, view, own: { client, view, userId: "u1" }, mine: () => view.user()! };
}
