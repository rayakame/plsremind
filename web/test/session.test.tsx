import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { renderApp, settle, wireUser } from "./support.js";

afterEach(cleanup);

test("nobody signed in sees the login form", async () => {
  await renderApp({ user: null });
  expect(screen.getByLabelText("Username")).toBeTruthy();
  expect(screen.getByLabelText("Password")).toBeTruthy();
});

test("a fresh instance asks for its first account", async () => {
  await renderApp({ user: null, info: { setup_required: true } });
  expect(screen.getByRole("heading", { name: "Set up plsremind" })).toBeTruthy();
});

test("the sidebar shows the lists with their open items", async () => {
  await renderApp();
  const nav = screen.getByRole("navigation");
  expect(within(nav).getByRole("link", { name: /All items/ })).toBeTruthy();
  expect(within(nav).getByRole("link", { name: /Inbox/ }).textContent).toContain("2");
  expect(within(nav).getByRole("link", { name: /Work/ }).textContent).toContain("0");
  expect(within(nav).getByRole("link", { name: "Calendar" })).toBeTruthy();
  expect(within(nav).getByRole("link", { name: "Done" })).toBeTruthy();
  expect(within(nav).getByRole("link", { name: "Settings" })).toBeTruthy();
});

test("a lost connection is said quietly and the data stays", async () => {
  const app = await renderApp();
  expect(screen.queryByText(/Reconnecting/)).toBeNull();
  act(() => app.gateway.current.drop());
  expect(screen.getByText(/Reconnecting/)).toBeTruthy();
  expect(screen.getByRole("link", { name: /Inbox/ })).toBeTruthy();
  await waitFor(() => expect(app.gateway.sockets).toHaveLength(2), { timeout: 3000 });
  await act(async () => {
    const socket = await app.gateway.opened();
    socket.hello();
    socket.dispatch("RESUMED", {}, 3);
    await settle();
  });
  expect(screen.queryByText(/Reconnecting/)).toBeNull();
});

test("a session that ended leads to the login and remembers where one was", async () => {
  const app = await renderApp({ path: "/lists/l2" });
  app.setUser(null);
  await act(async () => {
    app.gateway.current.drop(4004, "session ended");
    await settle();
  });
  await waitFor(() => expect(screen.getByLabelText("Username")).toBeTruthy());
  expect(app.path()).toBe("/login?next=%2Flists%2Fl2");
});

test("a first connection that fails is tried again", async () => {
  const app = await renderApp({ hold: true });
  await act(async () => {
    (await app.gateway.opened()).drop();
    await settle();
  });
  expect(screen.getByText(/Connecting/)).toBeTruthy();
  await waitFor(() => expect(app.gateway.sockets).toHaveLength(2), { timeout: 4000 });
  await act(() => app.connect());
  expect(screen.getByRole("link", { name: /Inbox/ })).toBeTruthy();
});

test("a write refused for want of a login leads to the login", async () => {
  const app = await renderApp({ user: wireUser({ locale: "en", timezone: "Pacific/Kiritimati" }) });
  app.route("PATCH", "/users/@me", { status: 401, body: { error: { code: "unauthorized" } } });
  app.setUser(null);
  fireEvent.click(screen.getByRole("button", { name: /^Use / }));
  await waitFor(() => expect(screen.getByLabelText("Username")).toBeTruthy());
});

test("the profile's language and theme apply", async () => {
  await renderApp({ user: wireUser({ locale: "de", preferences: { theme: "dark" } }) });
  expect(screen.getByRole("link", { name: /Alle Einträge/ })).toBeTruthy();
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(document.documentElement.lang).toBe("de");
});

test("an address nobody knows says so", async () => {
  await renderApp({ path: "/nowhere" });
  expect(screen.getByRole("heading", { name: "Not found" })).toBeTruthy();
});
