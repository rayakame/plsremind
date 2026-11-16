import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { change, renderApp, wireItem, wireList } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-11T11:00:00Z"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const rows = () => screen.getAllByRole("listitem").map((row) => row.textContent);
const titles = () =>
  screen.getAllByRole("listitem").map((row) => within(row).getByRole("link").textContent);

test("open items are grouped by how soon they are due", async () => {
  await renderApp({
    sync: {
      items: [
        wireItem({ id: "none", title: "Someday" }),
        wireItem({ id: "later", title: "Next month", due_at: "2026-12-16T08:00:00Z" }),
        wireItem({ id: "tonight", title: "Tonight", due_at: "2026-11-11T20:00:00Z" }),
        wireItem({
          id: "missed",
          title: "Missed",
          due_at: "2026-11-10T22:59:59Z",
          due_date: "2026-11-10",
          all_day: true,
          priority: "high",
        }),
        wireItem({ id: "thu", title: "Thursday", due_at: "2026-11-12T08:00:00Z", list_id: "l2" }),
      ],
    },
  });
  expect(screen.getByRole("heading", { level: 1, name: "All items" })).toBeTruthy();
  expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(
    expect.arrayContaining(["Overdue", "Today", "This week", "Later", "No date"]),
  );
  expect(rows()).toEqual([
    "MissedInboxHighyesterday",
    "TonightInboxtoday 9:00 PM",
    "ThursdayWorktomorrow 9:00 AM",
    "Next monthInboxDec 16 9:00 AM",
    "SomedayInbox",
  ]);
});

test("completing an item takes it out of the list at once", async () => {
  const app = await renderApp();
  app.route("PATCH", "/users/@me/items/b", { status: 200, body: wireItem({ id: "b" }) });
  fireEvent.click(screen.getByRole("checkbox", { name: "Complete Call Mia" }));
  expect(titles()).toEqual(["Hand in the report"]);
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/b")).toHaveLength(1));
  expect(app.sent("PATCH", "/users/@me/items/b")[0]!.body).toEqual({ status: "done" });
});

test("a completion the hub refuses comes back with the reason", async () => {
  const app = await renderApp();
  app.route("PATCH", "/users/@me/items/b", {
    status: 404,
    body: { error: { code: "not_found", message: "The resource does not exist." } },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "Complete Call Mia" }));
  expect((await screen.findByRole("alert")).textContent).toBe("This no longer exists.");
  expect(titles()).toEqual(["Hand in the report", "Call Mia"]);

  app.route("PATCH", "/users/@me/items/a", {
    status: 409,
    body: { error: { code: "from_the_future", message: "Said by the hub." } },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "Complete Hand in the report" }));
  await waitFor(() =>
    expect(screen.getAllByRole("alert").map((alert) => alert.textContent)).toContain(
      "Said by the hub.",
    ),
  );
});

test("what a user typed is shown as text, wherever it appears", async () => {
  const markup = '<img src=x onerror="alert(1)"><b>bold</b>';
  await renderApp({
    sync: {
      lists: [wireList({ name: markup })],
      items: [wireItem({ id: "a", title: markup }), wireItem({ id: "b", title: "x".repeat(400) })],
    },
  });
  expect(titles()[0]).toBe(markup);
  expect(screen.getByRole("navigation").textContent).toContain(markup);
  expect(document.querySelector("img, b")).toBeNull();
});

test("a long list draws only what is in view", async () => {
  const items = Array.from({ length: 5000 }, (_, i) =>
    wireItem({ id: `i${i}`, title: `Item ${i}` }),
  );
  await renderApp({ sync: { items } });
  expect(screen.getAllByRole("listitem").length).toBeLessThan(100);
  expect(screen.getByRole("link", { name: /All items/ }).textContent).toContain("5000");
});

test("clicking an item selects it, and the keyboard opens its page", async () => {
  const app = await renderApp();
  const link = screen.getByRole("link", { name: "Call Mia" });
  fireEvent.click(link, { detail: 1 });
  expect(link.closest("[role=listitem]")!.getAttribute("aria-current")).toBe("true");
  expect(app.path()).toBe("/");
  fireEvent.click(link, { detail: 0 });
  expect(app.path()).toBe("/items/b");
});

test("a list shows its own items and can be renamed, moved and deleted", async () => {
  const app = await renderApp({
    path: "/lists/l2",
    sync: {
      items: [wireItem({ id: "a", list_id: "l2", title: "At work" }), wireItem({ id: "b" })],
    },
  });
  app.route("PATCH", "/users/@me/lists/l2", { status: 200, body: wireList({ id: "l2" }) });
  app.route("DELETE", "/users/@me/lists/l2", { status: 204 });
  expect(screen.getByRole("heading", { level: 1, name: "Work" })).toBeTruthy();
  expect(titles()).toEqual(["At work"]);

  await userEvent.click(screen.getByRole("button", { name: "Rename" }));
  const name = within(screen.getByRole("dialog")).getByLabelText("Name");
  await userEvent.clear(name);
  await userEvent.type(name, "Job{Enter}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("heading", { level: 1, name: "Job" })).toBeTruthy();
  expect(app.sent("PATCH", "/users/@me/lists/l2")[0]!.body).toEqual({ name: "Job" });

  await userEvent.click(screen.getByRole("button", { name: "Delete list" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  expect(app.path()).toBe("/");
  expect(titles()).toEqual(["At work", "Hand in the report"]);
});

test("the inbox cannot be deleted and an unknown list says so", async () => {
  const app = await renderApp({ path: "/lists/l1" });
  expect(screen.getByRole("heading", { level: 1, name: "Inbox" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Delete list" })).toBeNull();
  act(() => app.location.navigate("/lists/gone"));
  expect(screen.getByRole("heading", { level: 1, name: "Not found" })).toBeTruthy();
});

test("the order within a list can follow the priority", async () => {
  await renderApp({
    path: "/lists/l1",
    sync: {
      items: [
        wireItem({ id: "a", title: "Soon", due_at: "2026-11-11T12:00:00Z" }),
        wireItem({ id: "b", title: "Urgent", due_at: "2026-11-11T18:00:00Z", priority: "high" }),
      ],
    },
  });
  expect(titles()).toEqual(["Soon", "Urgent"]);
  await userEvent.click(screen.getByRole("button", { name: "Priority" }));
  expect(titles()).toEqual(["Urgent", "Soon"]);
});

test("a new list is created from the sidebar and opened", async () => {
  const app = await renderApp();
  app.route("POST", "/users/@me/lists", (call) => {
    const list = wireList({ id: "l9", name: "Errands", position: 2, is_inbox: false });
    queueMicrotask(
      () => void app.send("list.created", change("u1", list, call.headers["x-request-nonce"]!)),
    );
    return { status: 201, body: list };
  });
  await userEvent.click(screen.getByRole("button", { name: "New list" }));
  await userEvent.type(within(screen.getByRole("dialog")).getByLabelText("Name"), "Errands{Enter}");
  await waitFor(() => expect(app.path()).toBe("/lists/l9"));
  expect(screen.getByRole("heading", { level: 1, name: "Errands" })).toBeTruthy();
});

test("an empty view says so", async () => {
  await renderApp({ sync: { items: [] } });
  expect(screen.getByText("Nothing open.")).toBeTruthy();
});

test("the group headings follow the profile's language", async () => {
  const { wireUser } = await import("./support.js");
  await renderApp({ user: wireUser({ locale: "de" }) });
  expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toContain(
    "Ohne Datum",
  );
});

test("the inbox cannot be renamed", async () => {
  await renderApp({ path: "/lists/l1" });
  expect(screen.queryByRole("button", { name: "Rename" })).toBeNull();
});

test("a row keeps the focus when its draft becomes the real item", async () => {
  const app = await renderApp();
  let nonce = "";
  app.route("POST", "/users/@me/items", (call) => {
    nonce = call.headers["x-request-nonce"]!;
    return new Promise(() => {});
  });
  await userEvent.type(
    screen.getByRole("combobox", { name: /Search, create/ }),
    "Fresh{Control>}{Enter}{/Control}",
  );
  const box = screen.getByRole("checkbox", { name: "Complete Fresh" });
  box.focus();
  await waitFor(() => expect(nonce).not.toBe(""));
  await app.send("item.created", change("u1", wireItem({ id: "real", title: "Fresh" }), nonce));
  expect(screen.getByRole("checkbox", { name: "Complete Fresh" })).toBe(box);
  expect(document.activeElement).toBe(box);
});
