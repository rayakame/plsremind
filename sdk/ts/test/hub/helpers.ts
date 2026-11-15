import { randomBytes } from "node:crypto";
import pg from "pg";
import { inject } from "vitest";
import { Client, RestClient, type Intent, type User } from "../../src/index.js";
import type { SocketLike } from "../../src/gateway.js";

export const hub = inject("hub");

export const everything: Intent[] = ["users", "lists", "items", "plans", "history", "deliveries"];

let addresses = 0;

/** The hub counts anonymous requests per address, so every caller in a test gets its own. */
function address(): string {
  addresses++;
  const salt = randomBytes(2);
  return `10.${salt[0]}.${salt[1]}.${(addresses % 250) + 1}`;
}

export function unique(name: string): string {
  return `${name}-${randomBytes(4).toString("hex")}`;
}

interface Caller {
  fetch: typeof fetch;
  headers: () => Record<string, string>;
}

function caller(): Caller {
  const from = address();
  let cookie = "";
  const headers = () => ({
    "x-forwarded-for": from,
    origin: hub.url,
    ...(cookie ? { cookie } : {}),
  });
  const wrapped: typeof fetch = async (input, init = {}) => {
    const response = await fetch(input, {
      ...init,
      headers: { ...(init.headers as Record<string, string>), ...headers() },
    });
    const set = response.headers.getSetCookie();
    if (set.length > 0) cookie = set.map((line) => line.split(";")[0]).join("; ");
    return response;
  };
  return { fetch: wrapped, headers };
}

export interface Session {
  rest: RestClient;
  user: User;
  /** A gateway client that uses this login instead of a token. */
  client(intents?: Intent[]): Tapped;
}

function sessionOf(from: Caller, user: User): Session {
  const rest = new RestClient({ url: hub.url, auth: "session", fetch: from.fetch });
  return {
    rest,
    user,
    client: (intents = everything) => {
      const tap = socketTap(from.headers);
      const client = new Client(
        { url: hub.url, auth: "session", intents, fetch: from.fetch },
        { createSocket: tap.createSocket },
      );
      return track({ client, tap });
    },
  };
}

let adminSession: Promise<Session> | undefined;

export function admin(): Promise<Session> {
  adminSession ??= (async () => {
    const from = caller();
    const rest = new RestClient({ url: hub.url, auth: "session", fetch: from.fetch });
    const user = await rest.login({ username: "admin", password: hub.adminPassword });
    await rest.updateSettings({ registration: "open" });
    return sessionOf(from, user);
  })();
  return adminSession;
}

/** A fresh user, logged in. */
export async function session(name: string): Promise<Session> {
  await admin();
  const from = caller();
  const rest = new RestClient({ url: hub.url, auth: "session", fetch: from.fetch });
  const user = await rest.register({
    username: unique(name),
    password: randomBytes(18).toString("base64"),
    displayName: name,
    timezone: "Europe/Berlin",
  });
  return sessionOf(from, user);
}

export async function personalToken(rest: RestClient, remind = false): Promise<string> {
  const created = await rest.createToken({ name: unique("adapter"), remind });
  return created.token!;
}

export async function connection(
  provider: string,
  allowedIntents?: Intent[],
): Promise<{ id: string; token: string }> {
  const { rest } = await admin();
  const created = await rest.createConnection({
    name: unique("bot"),
    provider,
    ...(allowedIntents ? { allowedIntents } : {}),
  });
  return { id: created.id, token: created.token! };
}

async function sql(text: string, values: unknown[]): Promise<void> {
  const client = new pg.Client({ connectionString: hub.databaseUrl });
  await client.connect();
  try {
    await client.query(text, values);
  } finally {
    await client.end();
  }
}

/** Links an account elsewhere to a user without going through OAuth. */
export async function giveIdentity(userId: string, provider: string, externalId: string) {
  await sql(
    `INSERT INTO identities (id, user_id, provider, external_id, display_name, created_at)
     VALUES (gen_random_uuid(), $1, $2, $3, $3, now())`,
    [userId, provider, externalId],
  );
}

/** Marks open items of a user as completed in the past, one second apart, newest first. */
export async function completeInThePast(userId: string, count: number) {
  await sql(
    `INSERT INTO items (id, user_id, list_id, title, status, completed_at, created_at, updated_at)
     SELECT gen_random_uuid(), $1, (SELECT id FROM lists WHERE user_id = $1 AND is_inbox),
            'old ' || n, 'done', now() - make_interval(secs => n), now(), now()
     FROM generate_series(1, $2) AS n`,
    [userId, count],
  );
}

export interface Tap {
  createSocket: (url: string) => SocketLike;
  /** Cuts the connection the way a network would. */
  dropCurrent(): void;
  opened(): number;
}

function socketTap(headers: () => Record<string, string>): Tap {
  const sockets: WebSocket[] = [];
  return {
    createSocket: (url) => {
      const options = { headers: headers() } as unknown as string[];
      const socket = new WebSocket(url, options);
      sockets.push(socket);
      return socket as unknown as SocketLike;
    },
    dropCurrent: () => sockets.at(-1)?.close(),
    opened: () => sockets.length,
  };
}

export interface Tapped {
  client: Client;
  tap: Tap;
}

const open = new Set<Client>();

function track(tapped: Tapped): Tapped {
  open.add(tapped.client);
  return tapped;
}

/** Closes every client a test made. Call it from afterEach. */
export function closeAll(): void {
  for (const client of open) client.close();
  open.clear();
}

export function tokenClient(token: string, intents: Intent[] = everything): Tapped {
  const from = caller();
  const tap = socketTap(() => ({ "x-forwarded-for": from.headers()["x-forwarded-for"]! }));
  const client = new Client(
    { url: hub.url, token, intents, fetch: from.fetch },
    { createSocket: tap.createSocket },
  );
  return track({ client, tap });
}

export function tokenRest(token: string): RestClient {
  return new RestClient({ url: hub.url, token, fetch: caller().fetch });
}

export async function until<T>(
  what: string,
  attempt: () => T | undefined | false | null | Promise<T | undefined | false | null>,
  timeoutMs = 10_000,
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const result = await attempt();
    if (result) return result;
    if (Date.now() > deadline) throw new Error(`Timed out waiting for ${what}.`);
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
}

export async function restartHub(): Promise<void> {
  const response = await fetch(`${hub.controlUrl}/restart`, { method: "POST" });
  if (response.status !== 204) throw new Error(`restarting the hub answered ${response.status}`);
}
