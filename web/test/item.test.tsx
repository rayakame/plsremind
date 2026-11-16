import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { change, deferred, renderApp, wireItem, wireList, wirePlan, wireUser } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-11T11:00:00Z"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const entry = (id: string, more: Record<string, unknown> = {}) => ({
  id,
  item_id: "a",
  kind: "before",
  offset: "1d",
  scheduled_for: "2026-11-10T18:00:00Z",
  fired_at: "2026-11-10T18:00:01Z",
  outcome: "fired",
  deliveries: [
    {
      id: "d1",
      channel: { kind: "connection", id: "c1", name: "Discord" },
      status: "delivered",
      reason: "",
      attempts: 1,
    },
  ],
  ...more,
});

test("an item's page shows it whole, with what was sent so far", async () => {
  const app = await renderApp({
    path: "/items/a",
    sync: { items: [wireItem({ id: "a", note: "<script>alert(1)</script>" })] },
    hold: true,
  });
  app.route("GET", "/users/@me/reminder-log", (call) =>
    call.path.includes("cursor=next")
      ? {
          status: 200,
          body: {
            data: [
              entry("e2", { kind: "due", offset: undefined, outcome: "missed", deliveries: [] }),
            ],
            next_cursor: null,
          },
        }
      : { status: 200, body: { data: [entry("e1")], next_cursor: "next" } },
  );
  await act(() => app.connect());
  expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Hand in the report");
  expect((screen.getByLabelText("Note") as HTMLTextAreaElement).value).toBe(
    "<script>alert(1)</script>",
  );
  expect(document.querySelector("main script")).toBeNull();
  const log = within(screen.getByRole("region", { name: "Sent reminders" }));
  await waitFor(() => expect(log.getAllByRole("listitem")).toHaveLength(1));
  expect(log.getByRole("listitem").textContent).toBe(
    "1d before" + "yesterday 7:00 PM" + "Discord: delivered",
  );
  expect(app.sent("GET", "/users/@me/reminder-log")[0]!.path).toContain("item_id=a");
  await userEvent.click(log.getByRole("button", { name: "Load more" }));
  await waitFor(() => expect(log.getAllByRole("listitem")).toHaveLength(2));
  expect(log.getAllByRole("listitem")[1]!.textContent).toBe(
    "When due" + "yesterday 7:00 PM" + "Missed",
  );
  expect(log.queryByRole("button", { name: "Load more" })).toBeNull();
});

test("previous and next follow the view one came from", async () => {
  const app = await renderApp();
  fireEvent.click(screen.getByRole("link", { name: "Hand in the report" }), { detail: 0 });
  expect(app.path()).toBe("/items/a");
  expect((screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement).disabled).toBe(
    true,
  );
  await userEvent.click(screen.getByRole("button", { name: "Next" }));
  expect(app.path()).toBe("/items/b");
  expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Call Mia");
  expect((screen.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  expect(app.location.history).toEqual(["/", "/items/b"]);
});

test("without a view to come from there is no previous and next", async () => {
  await renderApp({ path: "/items/a" });
  expect(screen.queryByRole("button", { name: "Next" })).toBeNull();
});

test("an item nobody has says so, and one deleted meanwhile too", async () => {
  const app = await renderApp({ path: "/items/zzz", hold: true });
  app.route("GET", "/users/@me/items/zzz", { status: 404, body: { error: { code: "not_found" } } });
  await act(() => app.connect());
  expect(await screen.findByRole("heading", { name: "Not found" })).toBeTruthy();

  act(() => app.location.navigate("/items/a"));
  expect(screen.getByLabelText("Title")).toBeTruthy();
  await app.send("item.deleted", change("u1", { id: "a" }));
  expect(screen.getByRole("heading", { name: "Not found" })).toBeTruthy();
});

test("an old completed item is fetched and can be reopened", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  app.route("GET", "/users/@me/items/old", { status: 200, body: old });
  app.route("PATCH", "/users/@me/items/old", { status: 200, body: { ...old, status: "open" } });
  await act(() => app.connect());
  expect(((await screen.findByLabelText("Title")) as HTMLInputElement).value).toBe("Long ago");
  expect((screen.getByLabelText("Title") as HTMLInputElement).readOnly).toBe(true);
  await userEvent.click(screen.getByRole("button", { name: "Reopen" }));
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/old")).toHaveLength(1));
  expect(app.sent("PATCH", "/users/@me/items/old")[0]!.body).toEqual({ status: "open" });
});

test("an item is completed and deleted from its page", async () => {
  const app = await renderApp({ path: "/items/a" });
  app.route("PATCH", "/users/@me/items/a", { status: 200, body: wireItem({ id: "a" }) });
  app.route("DELETE", "/users/@me/items/a", { status: 204 });
  await userEvent.click(screen.getByRole("button", { name: "Complete" }));
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/a")).toHaveLength(1));
  expect(screen.getByRole("button", { name: "Reopen" })).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Delete item" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  expect(app.path()).toBe("/");
  await waitFor(() => expect(app.sent("DELETE", "/users/@me/items/a")).toHaveLength(1));
});

test("an item that could not be loaded says so and loads on a second try", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  let asked = 0;
  app.route("GET", "/users/@me/items/old", () =>
    ++asked === 1
      ? { status: 500, body: { error: { code: "internal", message: "no" } } }
      : { status: 200, body: old },
  );
  await act(() => app.connect());
  expect(
    await screen.findByRole("heading", { name: "This item could not be loaded." }),
  ).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(((await screen.findByLabelText("Title")) as HTMLInputElement).value).toBe("Long ago");
});

test("a change to an item only its page has loaded shows at once and is taken back if refused", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  app.route("GET", "/users/@me/items/old", { status: 200, body: old });
  let asked = 0;
  app.route("PATCH", "/users/@me/items/old", () =>
    ++asked === 1
      ? { status: 500, body: { error: { code: "internal", message: "no" } } }
      : new Promise(() => {}),
  );
  await act(() => app.connect());
  await userEvent.click(await screen.findByRole("button", { name: "Reopen" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Reopen" })).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Reopen" }));
  expect(screen.getByRole("button", { name: "Complete" })).toBeTruthy();
});

test("after the hub answered, an item only its page holds is read again and never shows the old state", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  const open = { ...old, status: "open", completed_at: null, priority: "high" };
  let reads = 0;
  app.route("GET", "/users/@me/items/old", () => ({
    status: 200,
    body: ++reads === 1 ? old : open,
  }));
  app.route("PATCH", "/users/@me/items/old", { status: 200, body: open });
  await act(() => app.connect());
  const seen: string[] = [];
  const watch = new MutationObserver(() => {
    if (screen.queryByRole("button", { name: "Reopen" })) seen.push("old");
  });
  await userEvent.click(await screen.findByRole("button", { name: "Reopen" }));
  watch.observe(document.body, { childList: true, subtree: true, characterData: true });
  await waitFor(() => expect(reads).toBe(2));
  await waitFor(() =>
    expect((screen.getByLabelText("Priority") as HTMLSelectElement).value).toBe("high"),
  );
  watch.disconnect();
  expect(seen).toEqual([]);
  expect(screen.getByRole("button", { name: "Complete" })).toBeTruthy();
});

const emptyState = {
  user_id: "u1",
  chunk: { index: 0, count: 1 },
  user: wireUser({ locale: "en" }),
  lists: [wireList()],
  plans: [wirePlan({ priority: "low" }), wirePlan(), wirePlan({ priority: "high" })],
  items: [],
  history: [],
  history_cursor: null,
};

test("an item that a new sync no longer carries is read again, not taken for deleted", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  const high = { ...old, priority: "high" };
  let reads = 0;
  app.route("GET", "/users/@me/items/old", () => ({
    status: 200,
    body: ++reads === 1 ? old : high,
  }));
  const answer = deferred<{ status: number; body: unknown }>();
  app.route("PATCH", "/users/@me/items/old", () => answer.promise);
  await act(() => app.connect());
  await screen.findByLabelText("Title");
  await userEvent.type(
    screen.getByRole("combobox", { name: /Search, create/ }),
    "/prio high{Enter}",
  );
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/old")).toHaveLength(1));
  const nonce = app.sent("PATCH", "/users/@me/items/old")[0]!.headers["x-request-nonce"]!;
  await app.send("item.updated", change("u1", high, nonce));
  await app.send("user.sync", emptyState);
  expect(screen.queryByRole("heading", { name: "Not found" })).toBeNull();
  await act(async () => {
    answer.resolve({ status: 200, body: high });
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await waitFor(() =>
    expect((screen.getByLabelText("Priority") as HTMLSelectElement).value).toBe("high"),
  );
  expect(screen.queryByRole("heading", { name: "Not found" })).toBeNull();
});

test("a change answered after a sync and before its event keeps showing", async () => {
  const app = await renderApp({ path: "/items/old", hold: true });
  const old = wireItem({
    id: "old",
    title: "Long ago",
    status: "done",
    completed_at: "2026-01-01T10:00:00Z",
  });
  const high = { ...old, priority: "high" };
  let reads = 0;
  app.route("GET", "/users/@me/items/old", () => ({
    status: 200,
    body: ++reads === 1 ? old : high,
  }));
  const answer = deferred<{ status: number; body: unknown }>();
  app.route("PATCH", "/users/@me/items/old", () => answer.promise);
  await act(() => app.connect());
  await screen.findByLabelText("Title");
  await userEvent.type(
    screen.getByRole("combobox", { name: /Search, create/ }),
    "/prio high{Enter}",
  );
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/old")).toHaveLength(1));
  const priority = () => (screen.getByLabelText("Priority") as HTMLSelectElement).value;
  expect(priority()).toBe("high");
  await app.send("user.sync", emptyState);
  expect(priority()).toBe("high");
  await act(async () => {
    answer.resolve({ status: 200, body: high });
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(priority()).toBe("high");
  const nonce = app.sent("PATCH", "/users/@me/items/old")[0]!.headers["x-request-nonce"]!;
  await app.send("item.updated", change("u1", high, nonce));
  expect(priority()).toBe("high");
});
