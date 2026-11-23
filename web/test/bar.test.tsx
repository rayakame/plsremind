import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { change, deferred, renderApp, wireItem, wireList } from "./support.js";

afterEach(cleanup);

const bar = () => screen.getByRole("combobox", { name: "Search, create or type a command" });
const titles = () =>
  screen.queryAllByRole("listitem").map((row) => within(row).getByRole("link").textContent);
const done = (id: string, title: string) =>
  wireItem({ id, title, status: "done", completed_at: "2026-11-10T10:00:00Z" });

test("typing filters the view at once and adds matching done items below", async () => {
  await renderApp({
    sync: {
      items: [
        wireItem({ id: "a", title: "Hand in the report" }),
        wireItem({ id: "b", title: "Call Mia" }),
      ],
      history: [done("h1", "Old report"), done("h2", "Old call")],
    },
  });
  await userEvent.type(bar(), "rep");
  expect(titles()).toEqual(["Hand in the report", "Old report"]);
  expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toContain("Done");
  await userEvent.type(bar(), "zzz");
  expect(titles()).toEqual([]);
  expect(screen.getByText("Nothing matches.")).toBeTruthy();
});

test("the typed text becomes an item in the open list, or the inbox", async () => {
  const app = await renderApp({ path: "/lists/l2" });
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  await userEvent.type(bar(), "Buy milk{Control>}{Enter}{/Control}");
  expect((bar() as HTMLInputElement).value).toBe("");
  expect(titles()).toEqual(["Buy milk"]);
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Buy milk",
    list_id: "l2",
  });

  await userEvent.click(screen.getByRole("link", { name: /All items/ }));
  await userEvent.type(bar(), "  Buy bread ");
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(2));
  expect(app.sent("POST", "/users/@me/items")[1]!.body).toEqual({
    title: "Buy bread",
    list_id: "l1",
  });
});

test("Create with an empty bar asks for the details", async () => {
  const app = await renderApp({ path: "/lists/l2" });
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  const form = within(screen.getByRole("dialog", { name: "New item" }));
  expect((form.getByLabelText("List") as HTMLSelectElement).value).toBe("l2");
  await userEvent.type(form.getByLabelText("Title"), "Plan the trip");
  await userEvent.selectOptions(form.getByLabelText("Priority"), "High");
  await userEvent.selectOptions(form.getByLabelText("List"), "Inbox");
  await userEvent.click(form.getByRole("button", { name: "Create" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Plan the trip",
    list_id: "l1",
    priority: "high",
  });
});

test("Escape empties the bar and gives the focus back", async () => {
  await renderApp();
  await userEvent.type(bar(), "call");
  expect(titles()).toEqual(["Call Mia"]);
  await userEvent.keyboard("{Escape}");
  expect((bar() as HTMLInputElement).value).toBe("");
  expect(document.activeElement).not.toBe(bar());
  expect(titles()).toHaveLength(2);
});

test("going somewhere else empties the bar", async () => {
  await renderApp();
  await userEvent.type(bar(), "call");
  await userEvent.click(screen.getByRole("link", { name: /Work/ }));
  expect((bar() as HTMLInputElement).value).toBe("");
});

test("the details form keeps showing a new list picked before the hub confirmed it", async () => {
  const app = await renderApp();
  const answer = deferred<{ status: number; body: unknown }>();
  let nonce = "";
  app.route("POST", "/users/@me/lists", (call) => {
    nonce = call.headers["x-request-nonce"]!;
    return answer.promise;
  });
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  await userEvent.click(screen.getByRole("button", { name: "New list" }));
  await userEvent.type(within(screen.getByRole("dialog")).getByLabelText("Name"), "Errands{Enter}");
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  const form = within(screen.getByRole("dialog", { name: "New item" }));
  await userEvent.selectOptions(form.getByLabelText("List"), "Errands");
  expect((form.getByLabelText("List") as HTMLSelectElement).value).toMatch(/^tmp_/);
  const list = wireList({ id: "l9", name: "Errands", position: 2, is_inbox: false });
  await app.send("list.created", change("u1", list, nonce));
  answer.resolve({ status: 201, body: list });
  await waitFor(() => expect((form.getByLabelText("List") as HTMLSelectElement).value).toBe("l9"));
  await userEvent.type(form.getByLabelText("Title"), "Buy stamps");
  await userEvent.click(form.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Buy stamps",
    list_id: "l9",
  });
});

test("the details form falls back to the inbox when a new list picked there is refused", async () => {
  const app = await renderApp();
  const answer = deferred<{ status: number; body: unknown }>();
  app.route("POST", "/users/@me/lists", () => answer.promise);
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  await userEvent.click(screen.getByRole("button", { name: "New list" }));
  await userEvent.type(within(screen.getByRole("dialog")).getByLabelText("Name"), "Errands{Enter}");
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  const form = within(screen.getByRole("dialog", { name: "New item" }));
  await userEvent.selectOptions(form.getByLabelText("List"), "Errands");
  answer.resolve({
    status: 409,
    body: { error: { code: "limit_reached", message: "Too many lists." } },
  });
  await waitFor(() => expect(form.queryByRole("option", { name: "Errands" })).toBeNull());
  await userEvent.type(form.getByLabelText("Title"), "Buy stamps");
  await userEvent.click(form.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Buy stamps",
    list_id: "l1",
  });
});
