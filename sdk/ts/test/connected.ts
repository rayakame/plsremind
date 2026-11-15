import { Client, type Intent } from "../src/index.js";
import { fakeFetch } from "./fake-fetch.js";
import { FakeGateway, ready } from "./fake-gateway.js";
import { sync, wireItem, wireList, wirePlan, wireUser } from "./fixtures.js";

const everything: Intent[] = ["users", "lists", "items", "plans", "history", "deliveries"];

/** A client that is connected to a fake gateway and holds one user's state. */
export async function connectedClient(
  parts: Record<string, unknown> = {},
  intents: Intent[] = everything,
) {
  const { fetch } = fakeFetch([
    { status: 200, body: { gateway_url: "wss://hub.test/api/v1/gateway" } },
  ]);
  const fake = new FakeGateway();
  const client = new Client(
    { url: "https://hub.test", token: "plsr_pat_x", fetch, intents },
    { createSocket: fake.createSocket, random: () => 0.5 },
  );
  const connecting = client.connect();
  const socket = await fake.opened();
  socket.hello();
  ready(socket, { intents });
  socket.dispatch(
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
    2,
  );
  await connecting;
  let seq = 2;
  const send = (name: string, data: unknown) => socket.dispatch(name, data, ++seq);
  return { client, socket, fake, send };
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}
