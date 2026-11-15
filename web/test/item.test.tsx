import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { change, renderApp, wireItem } from "./support.js";

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
    "1d before" + "yesterday 19:00" + "Discord: delivered",
  );
  expect(app.sent("GET", "/users/@me/reminder-log")[0]!.path).toContain("item_id=a");
  await userEvent.click(log.getByRole("button", { name: "Load more" }));
  await waitFor(() => expect(log.getAllByRole("listitem")).toHaveLength(2));
  expect(log.getAllByRole("listitem")[1]!.textContent).toBe(
    "When due" + "yesterday 19:00" + "Missed",
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
