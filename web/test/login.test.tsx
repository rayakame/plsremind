import { act, cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wireUser } from "./support.js";

afterEach(cleanup);

test("a wrong password is said next to the form and the name stays", async () => {
  const app = await renderApp({ user: null });
  app.route("POST", "/auth/login", {
    status: 401,
    body: { error: { code: "invalid_credentials", message: "Wrong credentials." } },
  });
  await userEvent.type(screen.getByLabelText("Username"), "mia");
  await userEvent.type(screen.getByLabelText("Password"), "nope{Enter}");
  expect((await screen.findByRole("alert")).textContent).toBe("Wrong username or password.");
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("mia");
  expect(app.sent("POST", "/auth/login")[0]!.body).toEqual({ username: "mia", password: "nope" });
});

test("signing in opens the page one came for", async () => {
  const app = await renderApp({ user: null, path: "/login?next=%2Flists%2Fl2" });
  app.route("POST", "/auth/login", () => {
    app.setUser(wireUser({ locale: "en" }));
    return { status: 200, body: wireUser({ locale: "en" }) };
  });
  await userEvent.type(screen.getByLabelText("Username"), "mia");
  await userEvent.type(screen.getByLabelText("Password"), "right{Enter}");
  await waitFor(() => expect(app.gateway.sockets).toHaveLength(1));
  await act(() => app.connect());
  expect(app.path()).toBe("/lists/l2");
  expect(screen.getByRole("link", { name: /Work/ })).toBeTruthy();
});

test("an address outside the dashboard is not followed after signing in", async () => {
  const app = await renderApp({ user: null, path: "/login?next=%2F%2Fevil.example%2Fx" });
  app.route("POST", "/auth/login", () => {
    app.setUser(wireUser({ locale: "en" }));
    return { status: 200, body: wireUser({ locale: "en" }) };
  });
  await userEvent.type(screen.getByLabelText("Username"), "mia");
  await userEvent.type(screen.getByLabelText("Password"), "right{Enter}");
  await waitFor(() => expect(app.gateway.sockets).toHaveLength(1));
  await act(() => app.connect());
  expect(app.path()).toBe("/");
});

test("other ways to sign in are links to the hub", async () => {
  await renderApp({
    user: null,
    info: {
      login_methods: [
        { key: "password", type: "password", name: "Password" },
        { key: "gh", type: "github", name: "GitHub" },
      ],
    },
  });
  expect(screen.getByRole("link", { name: "Continue with GitHub" }).getAttribute("href")).toBe(
    "http://hub.test/api/v1/auth/oauth/gh/start",
  );
});

test("signing in elsewhere remembers the page one came for", async () => {
  await renderApp({
    user: null,
    path: "/login?next=%2Fcalendar",
    info: { login_methods: [{ key: "gh", type: "github", name: "GitHub" }] },
  });
  const link = screen.getByRole("link", { name: "Continue with GitHub" });
  link.addEventListener("click", (event) => event.preventDefault());
  await userEvent.click(link);
  expect(sessionStorage.getItem("plsremind.next")).toMatch(/^\d+ \/calendar$/);
});

test("registering sends the browser's zone and language, and the invite code when one is needed", async () => {
  const app = await renderApp({ user: null, path: "/register", info: { registration: "invite" } });
  app.route("POST", "/auth/register", {
    status: 422,
    body: { error: { code: "invalid_invite", message: "The invite is not valid." } },
  });
  await userEvent.type(screen.getByLabelText("Username"), "mia");
  await userEvent.type(screen.getByLabelText("Password"), "a long password");
  await userEvent.type(screen.getByLabelText("Invite code"), "abc{Enter}");
  expect((await screen.findByRole("alert")).textContent).toBe(
    "This invite is not valid or has been used.",
  );
  expect(app.sent("POST", "/auth/register")[0]!.body).toEqual({
    username: "mia",
    password: "a long password",
    invite_code: "abc",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    locale: navigator.language,
  });
});

test("a failed sign-in elsewhere is explained on the login page", async () => {
  await renderApp({ user: null, path: "/?auth_error=login_disabled" });
  expect((await screen.findByRole("alert")).textContent).toBe("Signing in this way is turned off.");
});
