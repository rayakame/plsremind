import { randomBytes } from "node:crypto";
import { test as base, expect, type BrowserContext, type Page } from "@playwright/test";
import pg from "pg";
import { RestClient, type User } from "plsremind";
import { proxyHeader } from "../../sdk/ts/test/hub/launch.js";

export { expect };

export const hubUrl = process.env.PLSREMIND_E2E_URL!;
export const databaseUrl = process.env.PLSREMIND_E2E_DATABASE!;
export const admin = { username: "admin", password: "the e2e admin password" };

let addresses = 0;

/** The hub counts anonymous requests per address, so every caller gets its own. */
export function address(): string {
  addresses++;
  const salt = randomBytes(1);
  return `10.${salt[0]}.${(process.pid + (addresses >> 8)) % 256}.${addresses % 256}`;
}

export function unique(name: string): string {
  return `${name}-${randomBytes(4).toString("hex")}`;
}

export interface Account {
  rest: RestClient;
  user: User;
  username: string;
  password: string;
  cookie: { name: string; value: string };
}

/** A REST client that keeps its login cookie, the way a browser would. */
export function restSession(): { rest: RestClient; cookie: () => { name: string; value: string } } {
  const from = address();
  let cookie = "";
  const fetchWithCookie: typeof fetch = async (input, init = {}) => {
    const response = await fetch(input, {
      ...init,
      headers: {
        ...(init.headers as Record<string, string>),
        [proxyHeader]: from,
        origin: hubUrl,
        ...(cookie ? { cookie } : {}),
      },
    });
    const set = response.headers.getSetCookie();
    if (set.length > 0) cookie = set.map((line) => line.split(";")[0]).join("; ");
    return response;
  };
  return {
    rest: new RestClient({ url: hubUrl, auth: "session", fetch: fetchWithCookie }),
    cookie: () => {
      const at = cookie.indexOf("=");
      return { name: cookie.slice(0, at), value: cookie.slice(at + 1) };
    },
  };
}

/** Registers a new user over REST. */
export async function createAccount(name = "mia"): Promise<Account> {
  const session = restSession();
  const username = unique(name);
  const password = `pw ${randomBytes(9).toString("base64")}`;
  const user = await session.rest.register({
    username,
    password,
    timezone: "Europe/Berlin",
    locale: "en",
  });
  return { rest: session.rest, user, username, password, cookie: session.cookie() };
}

/** Puts an account's login into a browser context. */
export async function signIn(context: BrowserContext, account: Account): Promise<void> {
  await context.addCookies([{ ...account.cookie, url: hubUrl }]);
}

interface Fixtures {
  /** A fresh user who is already signed in on the page. */
  account: Account;
  /** What the page logged as an error, to assert on. */
  problems: string[];
}

export const test = base.extend<Fixtures>({
  context: async ({ context }, use) => {
    await context.setExtraHTTPHeaders({ [proxyHeader]: address() });
    await use(context);
  },
  account: async ({ context }, use) => {
    const account = await createAccount();
    await signIn(context, account);
    await use(account);
  },
  problems: async ({ page }, use) => {
    const problems: string[] = [];
    watch(page, problems);
    await use(problems);
  },
});

export function watch(page: Page, problems: string[]): void {
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(message.text());
  });
  page.on("pageerror", (error) => problems.push(String(error)));
}

async function control(what: "halt" | "resume"): Promise<void> {
  const response = await fetch(`${process.env.PLSREMIND_E2E_CONTROL}/${what}`, { method: "POST" });
  if (response.status !== 204) throw new Error(`${what} answered ${response.status}`);
}

export const haltHub = () => control("halt");
export const resumeHub = () => control("resume");

/** Opens the navigation where it is a drawer. */
export async function openNav(page: Page): Promise<void> {
  await page.locator("nav").waitFor({ state: "attached" });
  const menu = page.getByRole("button", { name: "Menu" });
  if ((await menu.isVisible()) && (await menu.getAttribute("aria-expanded")) === "false") {
    await menu.click();
  }
}

async function sql(text: string, values: unknown[]): Promise<void> {
  const client = new pg.Client({ connectionString: databaseUrl });
  await client.connect();
  try {
    await client.query(text, values);
  } finally {
    await client.end();
  }
}

/** Links an account at a provider straight in the database, as a finished link would, and returns its id. */
export async function seedIdentity(
  userId: string,
  provider: string,
  externalId: string,
): Promise<string> {
  const client = new pg.Client({ connectionString: databaseUrl });
  await client.connect();
  try {
    const result = await client.query<{ id: string }>(
      `INSERT INTO identities (id, user_id, provider, external_id, display_name, username, created_at)
       VALUES (gen_random_uuid(), $1, $2, $3, 'Mia Example', 'mia', now()) RETURNING id`,
      [userId, provider, externalId],
    );
    return result.rows[0]!.id;
  } finally {
    await client.end();
  }
}

/** Puts open items straight into the database, for amounts REST would take minutes for. */
export async function seedOpen(userId: string, count: number): Promise<void> {
  await sql(
    `INSERT INTO items (id, user_id, list_id, title, status, due_at, created_at, updated_at)
     SELECT gen_random_uuid(), $1, (SELECT id FROM lists WHERE user_id = $1 AND is_inbox),
            'Seeded ' || n, 'open',
            CASE WHEN n % 3 = 0 THEN NULL ELSE now() + make_interval(hours => n) END,
            now(), now()
     FROM generate_series(1, $2) AS n`,
    [userId, count],
  );
}

/** Puts completed items into the database, one second apart, the first being the newest. */
export async function seedDone(userId: string, count: number): Promise<void> {
  await sql(
    `INSERT INTO items (id, user_id, list_id, title, status, completed_at, created_at, updated_at)
     SELECT gen_random_uuid(), $1, (SELECT id FROM lists WHERE user_id = $1 AND is_inbox),
            'Done ' || n, 'done', now() - make_interval(secs => n), now(), now()
     FROM generate_series(1, $2) AS n`,
    [userId, count],
  );
}
