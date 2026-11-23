import { afterEach, expect, test, vi } from "vitest";
import { ApiError, NetworkError, RestClient } from "../src/index.js";
import { fakeFetch, type Answer } from "./fake-fetch.js";
import { wireItem } from "./fixtures.js";

function fake(answers: (Answer | Error)[], options: object = { token: "plsr_pat_x" }) {
  const { fetch, calls } = fakeFetch(answers);
  return { rest: new RestClient({ url: "https://hub.test/", fetch, ...options }), calls };
}

afterEach(() => {
  vi.useRealTimers();
});

test("a write goes to the caller's own path with token, nonce and the hub's field names", async () => {
  const { rest, calls } = fake([{ status: 200, body: wireItem({ id: "i1", status: "done" }) }]);
  const item = await rest.updateItem("i1", { status: "done", dueAt: null }, { nonce: "n1" });
  expect(calls[0]).toMatchObject({
    method: "PATCH",
    url: "https://hub.test/api/v1/users/@me/items/i1",
    headers: {
      authorization: "Bearer plsr_pat_x",
      "x-request-nonce": "n1",
      "content-type": "application/json",
    },
    body: { status: "done", due_at: null },
  });
  expect(item.status).toBe("done");
  expect(item.createdAt).toBeInstanceOf(Date);
});

test("a connection must name the user and is stopped before the hub is asked", async () => {
  const { rest, calls } = fake([{ status: 200, body: { data: [], next_cursor: null } }], {
    token: "plsr_con_x",
  });
  await expect(rest.listItems()).rejects.toBeInstanceOf(TypeError);
  expect(calls).toEqual([]);
  await rest.listItems({}, { user: "u1" });
  expect(calls[0]!.url).toBe("https://hub.test/api/v1/users/u1/items");
  await rest.listDeliveries().catch(() => {});
  expect(calls[1]!.url).toBe("https://hub.test/api/v1/deliveries");
});

test("list queries are written in the hub's form", async () => {
  const { rest, calls } = fake([{ status: 200, body: { data: [wireItem()], next_cursor: "c2" } }]);
  const page = await rest.listItems({
    listId: "l1",
    hasDue: true,
    dueBefore: new Date("2026-11-13T18:00:00Z"),
    sort: "completed",
    order: "desc",
    limit: 10,
  });
  const url = new URL(calls[0]!.url);
  expect(url.pathname).toBe("/api/v1/users/@me/items");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    list_id: "l1",
    has_due: "true",
    due_before: "2026-11-13T18:00:00.000Z",
    sort: "completed",
    order: "desc",
    limit: "10",
  });
  expect(page.nextCursor).toBe("c2");
  expect(page.data[0]!.listId).toBe("l1");
});

test("an error answer becomes an ApiError with SDK field names", async () => {
  const { rest } = fake([
    {
      status: 422,
      body: {
        error: {
          code: "validation_failed",
          message: "The request is invalid.",
          fields: { due_at: "Give due_at or due_date, not both." },
        },
      },
    },
  ]);
  const error = await rest.createItem({ title: "x" }).catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  expect(error).toMatchObject({
    status: 422,
    code: "validation_failed",
    message: "The request is invalid.",
    fields: { dueAt: "Give due_at or due_date, not both." },
  });
});

test("an error answer that is not the hub's JSON still carries the status", async () => {
  const { rest } = fake([{ status: 502, text: "<html>bad gateway</html>" }]);
  const error = await rest.getInfo().catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  expect(error).toMatchObject({ status: 502, code: "unknown" });
});

test("no answer becomes a NetworkError that carries the cause", async () => {
  const cause = new TypeError("fetch failed");
  const { rest } = fake([cause]);
  const error = await rest.getInfo().catch((e: unknown) => e);
  expect(error).toBeInstanceOf(NetworkError);
  expect((error as NetworkError).cause).toBe(cause);
});

test("an answer that cannot be read becomes a NetworkError", async () => {
  const { rest } = fake([{ status: 200, text: "<html>" }]);
  await expect(rest.getInfo()).rejects.toBeInstanceOf(NetworkError);
});

test("an aborted call rejects with the abort itself", async () => {
  const { rest } = fake([{ status: 200, body: {} }]);
  const controller = new AbortController();
  controller.abort();
  const error = await rest.getInfo({ signal: controller.signal }).catch((e: unknown) => e);
  expect((error as Error).name).toBe("AbortError");
});

test("a 429 is waited out and tried again, three times at most", async () => {
  vi.useFakeTimers();
  const limited: Answer = {
    status: 429,
    headers: { "retry-after": "2" },
    body: { error: { code: "rate_limited", message: "Too many requests. Try again later." } },
  };
  const ok = fake([limited, limited, limited, { status: 200, body: [] }]);
  const lists = ok.rest.listLists();
  await vi.advanceTimersByTimeAsync(1999);
  expect(ok.calls).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(4001);
  expect(await lists).toEqual([]);
  expect(ok.calls).toHaveLength(4);

  const never = fake([limited]);
  const failing = never.rest.listLists().catch((e: unknown) => e);
  await vi.advanceTimersByTimeAsync(10_000);
  expect(await failing).toMatchObject({ status: 429, code: "rate_limited" });
  expect(never.calls).toHaveLength(4);
});

test("a 429 that asks for 30 seconds or more is not waited out", async () => {
  const { rest, calls } = fake([
    { status: 429, headers: { "retry-after": "30" }, body: { error: { code: "rate_limited" } } },
  ]);
  await expect(rest.listLists()).rejects.toMatchObject({ status: 429 });
  expect(calls).toHaveLength(1);
});

test("retry: false hands the 429 straight back", async () => {
  const { rest, calls } = fake(
    [{ status: 429, headers: { "retry-after": "1" }, body: { error: { code: "rate_limited" } } }],
    { token: "plsr_pat_x", retry: false },
  );
  await expect(rest.listLists()).rejects.toMatchObject({ status: 429 });
  expect(calls).toHaveLength(1);
});

test("pages walks every page and stops at the last", async () => {
  const { rest, calls } = fake([
    { status: 200, body: { data: [wireItem({ id: "a" })], next_cursor: "c1" } },
    { status: 200, body: { data: [wireItem({ id: "b" })], next_cursor: null } },
  ]);
  const ids: string[] = [];
  for await (const page of rest.pages((cursor) => rest.listItems({ status: "done", cursor }))) {
    ids.push(...page.data.map((item) => item.id));
  }
  expect(ids).toEqual(["a", "b"]);
  expect(calls).toHaveLength(2);
  expect(new URL(calls[0]!.url).searchParams.has("cursor")).toBe(false);
  expect(new URL(calls[1]!.url).searchParams.get("cursor")).toBe("c1");
});

test("session auth sends the cookie and no token", async () => {
  const { rest, calls } = fake([{ status: 204 }], { auth: "session" });
  expect(await rest.logout()).toBeUndefined();
  expect(calls[0]!.credentials).toBe("include");
  expect(calls[0]!.headers.authorization).toBeUndefined();
});

test("token and session together are refused, and so is a missing address", () => {
  expect(() => new RestClient({ url: "https://hub.test", token: "t", auth: "session" })).toThrow(
    TypeError,
  );
  expect(() => new RestClient({ token: "t" })).toThrow(TypeError);
});

test("oauthStartUrl builds the address the browser goes to", () => {
  const { rest } = fake([]);
  expect(rest.oauthStartUrl("github", { intent: "link", inviteCode: "abc" })).toBe(
    "https://hub.test/api/v1/auth/oauth/github/start?intent=link&invite_code=abc",
  );
  expect(rest.oauthStartUrl("my idp")).toBe("https://hub.test/api/v1/auth/oauth/my%20idp/start");
});

test("createReminder and createInvite send durations as text", async () => {
  const { rest, calls } = fake([{ status: 201, body: {} }]);
  await rest.createReminder("i1", { in: 3_600_000 });
  await rest.createReminder("i1", { at: new Date("2026-11-13T18:00:00Z") });
  await rest.createInvite({ expiresIn: 86_400_000 });
  expect(calls.map((call) => call.body)).toEqual([
    { in: "1h" },
    { at: "2026-11-13T18:00:00.000Z" },
    { expires_in: "1d" },
  ]);
});

test("ids in a path are escaped", async () => {
  const { rest, calls } = fake([{ status: 204 }]);
  await rest.deleteProvider("a/b");
  expect(calls[0]!.url).toBe("https://hub.test/api/v1/admin/providers/a%2Fb");
});

test("a 429 that does not say how long to wait is not tried again", async () => {
  const { rest, calls } = fake([{ status: 429, body: { error: { code: "rate_limited" } } }]);
  await expect(rest.listLists()).rejects.toMatchObject({ status: 429 });
  expect(calls).toHaveLength(1);
});

test("@me as the user is the caller's own path", async () => {
  const { rest, calls } = fake([{ status: 200, body: [] }]);
  await rest.listLists({ user: "@me" });
  expect(calls[0]!.url).toBe("https://hub.test/api/v1/users/@me/lists");
});

test("an empty answer where an object was promised becomes a NetworkError", async () => {
  const { rest } = fake([{ status: 200 }]);
  await expect(rest.getInfo()).rejects.toBeInstanceOf(NetworkError);
  const gone = fake([{ status: 204 }]);
  await expect(gone.rest.deleteItem("i1")).resolves.toBeUndefined();
});

test("an abort that happens just before the wait for a retry is not slept through", async () => {
  const controller = new AbortController();
  const fetch = (async () => {
    controller.abort();
    return new Response(null, { status: 429, headers: { "retry-after": "29" } });
  }) as typeof globalThis.fetch;
  const rest = new RestClient({ url: "https://hub.test/", fetch, token: "plsr_pat_x" });
  const outcome = await Promise.race([
    rest.listLists({ signal: controller.signal }).catch((e: unknown) => (e as Error).name),
    new Promise((resolve) => setTimeout(() => resolve("still waiting"), 200)),
  ]);
  expect(outcome).toBe("AbortError");
});

test.each(["", ".", ".."])(
  "an id of %j would reach another call and is refused first",
  async (id) => {
    const { rest, calls } = fake([{ status: 204 }]);
    await expect(rest.deleteSubitem("item-1", id)).rejects.toBeInstanceOf(TypeError);
    await expect(rest.getItem(id)).rejects.toBeInstanceOf(TypeError);
    await expect(rest.deleteReminder(id, "reminder-1")).rejects.toBeInstanceOf(TypeError);
    await expect(rest.listLists({ user: id || ".." })).rejects.toBeInstanceOf(TypeError);
    expect(calls).toEqual([]);
  },
);

test("a test of an app is started, looked at and answered at its own paths", async () => {
  const test = {
    id: "t1",
    connection_id: "c1",
    status: "pending",
    reason: "",
    created_at: "2026-11-12T18:00:00Z",
    closed_at: null,
  };
  const { rest, calls } = fake([
    { status: 201, body: test },
    { status: 200, body: test },
  ]);
  const started = await rest.startConnectionTest("c1");
  expect(started.createdAt).toBeInstanceOf(Date);
  await rest.getConnectionTest("c1", "t1");
  expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
    "POST https://hub.test/api/v1/users/@me/connections/c1/tests",
    "GET https://hub.test/api/v1/users/@me/connections/c1/tests/t1",
  ]);

  const bot = fake([{ status: 200, body: { ...test, status: "failed", reason: "closed" } }], {
    token: "plsr_con_x",
  });
  await bot.rest.reportTest("t1", { status: "failed", reason: "closed" });
  expect(bot.calls[0]).toMatchObject({
    method: "PUT",
    url: "https://hub.test/api/v1/deliveries/tests/t1",
    body: { status: "failed", reason: "closed" },
  });
});

test("starting a test too soon is not waited out", async () => {
  const { rest, calls } = fake([
    {
      status: 429,
      headers: { "retry-after": "20" },
      body: { error: { code: "rate_limited", message: "Wait." } },
    },
  ]);
  await expect(rest.startConnectionTest("c1")).rejects.toMatchObject({
    status: 429,
    code: "rate_limited",
  });
  expect(calls).toHaveLength(1);
});

test("an app is created and changed with its install link", async () => {
  const app = {
    id: "c1",
    name: "Bot",
    description: "",
    provider: "discord",
    enabled: true,
    allowed_intents: [],
    install_url: "https://x.example",
    reach: 3,
    created_at: "2026-11-12T18:00:00Z",
    last_used_at: null,
  };
  const { rest, calls } = fake([
    { status: 201, body: app },
    { status: 200, body: app },
  ]);
  const created = await rest.createConnection({
    name: "Bot",
    provider: "discord",
    installUrl: "https://x.example",
  });
  expect(created.reach).toBe(3);
  await rest.updateConnection("c1", { installUrl: "" });
  expect(calls[0]!.body).toMatchObject({ install_url: "https://x.example" });
  expect(calls[1]!.body).toEqual({ install_url: "" });
});

test("the picture of a linked account has an address and comes as a blob", async () => {
  const { rest, calls } = fake([
    { status: 200, text: "png", headers: { "content-type": "image/png" } },
  ]);
  expect(rest.identityAvatarUrl("i1")).toBe(
    "https://hub.test/api/v1/users/@me/identities/i1/avatar",
  );
  const picture = await rest.getIdentityAvatar("i1");
  expect(picture).toBeInstanceOf(Blob);
  expect(await picture.text()).toBe("png");
  expect(calls[0]!.url).toBe("https://hub.test/api/v1/users/@me/identities/i1/avatar");
});
