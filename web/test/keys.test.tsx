import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { actions, bindings, defaults, rebind } from "../src/keys/actions.js";
import { renderApp, wireItem, wireUser } from "./support.js";

afterEach(cleanup);

const current = () =>
  screen
    .queryAllByRole("listitem")
    .filter((row) => row.getAttribute("aria-current") === "true")
    .map((row) => within(row).getByRole("link").textContent);
const bar = () => screen.getByRole("combobox", { name: /Search, create/ }) as HTMLInputElement;

const sync = {
  items: [
    wireItem({ id: "a", title: "First", due_at: "2020-01-01T10:00:00Z" }),
    wireItem({ id: "b", title: "Second", due_at: "2020-01-02T10:00:00Z" }),
    wireItem({ id: "c", title: "Third" }),
  ],
};

test("every action has one key and the defaults do not collide", () => {
  expect(actions).toHaveLength(12);
  expect(new Set(Object.values(defaults)).size).toBe(12);
  expect(bindings({}).get("x")).toBe("complete");
  expect(bindings({ complete: "c" }).get("c")).toBe("complete");
  expect(bindings({ complete: "c" }).has("x")).toBe(false);
  expect(bindings({ nonsense: "q" }).has("q")).toBe(false);
});

test("a key that is in use is refused, and the default is not stored", () => {
  expect(rebind({}, "complete", "j")).toEqual({ ok: false, conflict: "down" });
  expect(rebind({}, "complete", "c")).toEqual({ ok: true, overrides: { complete: "c" } });
  expect(rebind({ complete: "c" }, "complete", "x")).toEqual({ ok: true, overrides: {} });
  expect(rebind({ down: "n" }, "new", "j")).toEqual({
    ok: true,
    overrides: { down: "n", new: "j" },
  });
  expect(rebind({ down: "n" }, "up", "n")).toEqual({ ok: false, conflict: "down" });
  expect(rebind({}, "complete", "")).toEqual({ ok: false, conflict: null });
});

test("j, k and the arrows walk through the view across its groups", async () => {
  await renderApp({ sync });
  await userEvent.keyboard("j");
  expect(current()).toEqual(["First"]);
  await userEvent.keyboard("j{ArrowDown}");
  expect(current()).toEqual(["Third"]);
  await userEvent.keyboard("j");
  expect(current()).toEqual(["Third"]);
  await userEvent.keyboard("k{ArrowUp}{ArrowUp}");
  expect(current()).toEqual(["First"]);
});

test("x completes the selected item and Enter opens it", async () => {
  const app = await renderApp({ sync });
  app.route("PATCH", "/users/@me/items/a", { status: 200, body: wireItem({ id: "a" }) });
  await userEvent.keyboard("jx");
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/a")).toHaveLength(1));
  expect(app.sent("PATCH", "/users/@me/items/a")[0]!.body).toEqual({ status: "done" });
  await userEvent.keyboard("j{Enter}");
  expect(app.path()).toBe("/items/b");
  expect(screen.queryByRole("complementary", { name: "Details" })).toBeNull();
});

test("keys typed into a field are just text", async () => {
  const app = await renderApp({ sync });
  await userEvent.keyboard("j");
  await userEvent.type(bar(), "xjk?");
  expect(bar().value).toBe("xjk?");
  expect(app.sent("PATCH", /items/)).toHaveLength(0);
  expect(screen.queryByRole("dialog")).toBeNull();
  await userEvent.keyboard("{Escape}");
  expect(current()).toEqual(["First"]);
});

test("keys lead into the bar: search, the prefix and the commands about an item", async () => {
  await renderApp({ sync });
  await userEvent.keyboard("f");
  expect(document.activeElement).toBe(bar());
  expect(bar().value).toBe("");
  await userEvent.keyboard("{Escape}/");
  expect(document.activeElement).toBe(bar());
  expect(bar().value).toBe("/");
  for (const [key, text] of [
    ["n", "/new "],
    ["r", "/remind "],
    ["d", "/due "],
    ["m", "/move #"],
    ["g", "/go "],
  ]) {
    await userEvent.keyboard(`{Escape}${key}`);
    expect(bar().value).toBe(text);
  }
});

test("the question mark shows every key, and nothing else acts while it is open", async () => {
  const app = await renderApp({ sync });
  await userEvent.keyboard("?");
  const help = within(screen.getByRole("dialog", { name: "Keys" }));
  expect(help.getAllByRole("listitem")).toHaveLength(12);
  expect(help.getAllByRole("listitem")[3]!.textContent).toBe("Complete the selected item" + "x");
  await userEvent.keyboard("j");
  expect(current()).toEqual([]);
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(app.path()).toBe("/");
});

test("the user's own keys replace the defaults", async () => {
  const app = await renderApp({
    sync,
    user: wireUser({ locale: "en", preferences: { keys: { complete: "c" }, prefix: "!" } }),
  });
  app.route("PATCH", "/users/@me/items/a", { status: 200, body: wireItem({ id: "a" }) });
  await userEvent.keyboard("jx");
  expect(app.sent("PATCH", /items/)).toHaveLength(0);
  await userEvent.keyboard("c");
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/a")).toHaveLength(1));
  await userEvent.keyboard("!");
  expect(bar().value).toBe("!");
});

test("t goes through the colour schemes", async () => {
  const app = await renderApp({ sync });
  app.route("PATCH", "/users/@me", { status: 200, body: wireUser({ locale: "en" }) });
  await userEvent.keyboard("t");
  expect(document.documentElement.dataset.theme).toBe("light");
  await userEvent.keyboard("t");
  expect(document.documentElement.dataset.theme).toBe("dark");
  await waitFor(() => expect(app.sent("PATCH", "/users/@me")).toHaveLength(2));
});

test("a key with Ctrl, Alt or Meta is left to the browser", async () => {
  await renderApp({ sync });
  await userEvent.keyboard("{Control>}j{/Control}{Meta>}j{/Meta}{Alt>}j{/Alt}");
  expect(current()).toEqual([]);
});

test("on a page without a list the keys for moving do nothing", async () => {
  const app = await renderApp({ sync });
  await userEvent.keyboard("j");
  expect(current()).toEqual(["First"]);
  const { act } = await import("@testing-library/react");
  act(() => app.location.navigate("/nowhere"));
  expect(screen.queryByRole("complementary", { name: "Details" })).toBeNull();
  await userEvent.keyboard("j{ArrowDown}");
  expect(screen.queryByRole("complementary", { name: "Details" })).toBeNull();
});

test("keys still act after a checkbox was ticked", async () => {
  const app = await renderApp({ sync });
  app.route("PATCH", /items/, { status: 200, body: wireItem({ id: "a" }) });
  await userEvent.click(screen.getByRole("checkbox", { name: "Complete Third" }));
  await userEvent.keyboard("j");
  expect(current()).toEqual(["First"]);
});

test("a command on an item's page is about that item, whatever was selected before", async () => {
  const app = await renderApp({ sync });
  app.route("PATCH", /items/, { status: 200, body: wireItem({ id: "b" }) });
  await userEvent.keyboard("j");
  const { act } = await import("@testing-library/react");
  act(() => app.location.navigate("/items/b"));
  await userEvent.type(bar(), "/prio high{Enter}");
  await waitFor(() => expect(app.sent("PATCH", "/users/@me/items/b")).toHaveLength(1));
  expect(app.sent("PATCH", "/users/@me/items/a")).toHaveLength(0);
});
