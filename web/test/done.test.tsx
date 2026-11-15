import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { change, renderApp, wireItem, wireUser } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-11T11:00:00Z"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const done = (id: string, title: string, at: string, more = {}) =>
  wireItem({ id, title, status: "done", completed_at: at, ...more });
const titles = () =>
  screen.queryAllByRole("listitem").map((row) => within(row).getByRole("link").textContent);
const history = [
  done("h1", "This morning", "2026-11-11T08:00:00Z"),
  done("h2", "Yesterday evening", "2026-11-10T19:00:00Z", { list_id: "l2" }),
  done("h3", "Last month", "2026-10-20T10:00:00Z"),
];

test("done items are listed newest first, by how long ago", async () => {
  await renderApp({ path: "/done", sync: { history } });
  expect(screen.getByRole("heading", { level: 1, name: "Done" })).toBeTruthy();
  expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(
    expect.arrayContaining(["Today", "Yesterday", "Earlier"]),
  );
  expect(titles()).toEqual(["This morning", "Yesterday evening", "Last month"]);
  expect(screen.queryByRole("button", { name: "Load older" })).toBeNull();
});

test("the tick reopens an item and takes it out at once", async () => {
  const app = await renderApp({ path: "/done", sync: { history } });
  app.route("PATCH", "/users/@me/items/h1", { status: 200, body: wireItem({ id: "h1" }) });
  fireEvent.click(screen.getByRole("checkbox", { name: "Reopen This morning" }));
  expect(titles()).toEqual(["Yesterday evening", "Last month"]);
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/h1")).toHaveLength(1));
  expect(app.sent("PATCH", "/users/@me/items/h1")[0]!.body).toEqual({ status: "open" });
});

test("older items are loaded page by page from where the cache ends", async () => {
  const app = await renderApp({ path: "/done", sync: { history, history_cursor: "c1" } });
  app.route("GET", "/users/@me/items", (call) =>
    call.path.includes("cursor=c2")
      ? {
          status: 200,
          body: { data: [done("o3", "Oldest", "2026-01-01T10:00:00Z")], next_cursor: null },
        }
      : {
          status: 200,
          body: {
            data: [
              done("o1", "Older", "2026-09-01T10:00:00Z"),
              done("h3", "Last month", "2026-10-20T10:00:00Z"),
            ],
            next_cursor: "c2",
          },
        },
  );
  await userEvent.click(screen.getByRole("button", { name: "Load older" }));
  await waitFor(() => expect(titles()).toHaveLength(4));
  expect(titles()).toEqual(["This morning", "Yesterday evening", "Last month", "Older"]);
  const first = app.sent("GET", "/users/@me/items")[0]!.path;
  expect(first).toContain("status=done");
  expect(first).toContain("sort=completed");
  expect(first).toContain("order=desc");
  expect(first).toContain("cursor=c1");
  await userEvent.click(screen.getByRole("button", { name: "Load older" }));
  await waitFor(() => expect(titles()).toHaveLength(5));
  expect(app.sent("GET", "/users/@me/items")[1]!.path).toContain("cursor=c2");
  expect(screen.queryByRole("button", { name: "Load older" })).toBeNull();
});

test("a change of the time zone starts the older pages over", async () => {
  const app = await renderApp({ path: "/done", sync: { history, history_cursor: "c1" } });
  app.route("GET", "/users/@me/items", {
    status: 200,
    body: { data: [done("o1", "Older", "2026-09-01T10:00:00Z")], next_cursor: null },
  });
  await userEvent.click(screen.getByRole("button", { name: "Load older" }));
  await waitFor(() => expect(titles()).toHaveLength(4));
  await app.send(
    "user.updated",
    change("u1", wireUser({ locale: "en", timezone: "Pacific/Auckland" })),
  );
  expect(titles()).toHaveLength(3);
  expect(screen.getByRole("button", { name: "Load older" })).toBeTruthy();
});

test("the list and the bar narrow what is shown", async () => {
  await renderApp({ path: "/done", sync: { history } });
  await userEvent.selectOptions(screen.getByLabelText("List"), "Work");
  expect(titles()).toEqual(["Yesterday evening"]);
  await userEvent.selectOptions(screen.getByLabelText("List"), "All lists");
  await userEvent.type(screen.getByRole("combobox", { name: /Search, create/ }), "month");
  expect(titles()).toEqual(["Last month"]);
});

test("nothing done yet says so", async () => {
  await renderApp({ path: "/done" });
  expect(screen.getByText("Nothing done yet.")).toBeTruthy();
});

test("an older item that the cache does not hold opens on its own page and leaves when reopened", async () => {
  const app = await renderApp({ path: "/done", sync: { history, history_cursor: "c1" } });
  const older = done("o1", "Older", "2026-09-01T10:00:00Z");
  app.route("GET", "/users/@me/items", { status: 200, body: { data: [older], next_cursor: null } });
  app.route("GET", "/users/@me/items/o1", { status: 200, body: older });
  app.route("PATCH", "/users/@me/items/o1", { status: 200, body: { ...older, status: "open" } });
  await userEvent.click(screen.getByRole("button", { name: "Load older" }));
  await waitFor(() => expect(titles()).toHaveLength(4));
  fireEvent.click(screen.getByRole("checkbox", { name: "Reopen Older" }));
  expect(titles()).toHaveLength(3);
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/o1")).toHaveLength(1));
});

test("clicking an older item opens its page instead of an empty column", async () => {
  const app = await renderApp({ path: "/done", sync: { history, history_cursor: "c1" } });
  const older = done("o1", "Older", "2026-09-01T10:00:00Z");
  app.route("GET", "/users/@me/items", { status: 200, body: { data: [older], next_cursor: null } });
  app.route("GET", "/users/@me/items/o1", { status: 200, body: older });
  await userEvent.click(screen.getByRole("button", { name: "Load older" }));
  await waitFor(() => expect(titles()).toHaveLength(4));
  fireEvent.click(screen.getByRole("link", { name: "Older" }), { detail: 1 });
  expect(app.path()).toBe("/items/o1");
});
