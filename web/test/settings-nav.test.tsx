import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { Avatar } from "../src/ui/avatar.js";
import { renderApp, wireApp, wireUser } from "./support.js";

afterEach(() => {
  cleanup();
  document.documentElement.style.removeProperty("--layout");
});

const admin = wireUser({ locale: "en", role: "admin" });

async function settings(path: string, options: Parameters<typeof renderApp>[0] = {}) {
  const app = await renderApp({ path, ...options });
  await screen.findByRole("navigation", { name: "Settings" });
  return app;
}

function nav() {
  return within(screen.getByRole("navigation", { name: "Settings" }));
}

test("the settings come in groups, without the administration for others", async () => {
  await settings("/settings/profile");
  expect(
    nav()
      .getAllByRole("heading")
      .map((heading) => heading.textContent),
  ).toEqual(["Account", "Reminders", "Dashboard", "For developers"]);
  expect(
    nav()
      .getAllByRole("link")
      .map((link) => link.textContent),
  ).toEqual([
    "Profile",
    "Sign-in and security",
    "When and how often",
    "Apps",
    "Appearance",
    "Keyboard and commands",
    "API tokens",
  ]);
  expect(nav().getByRole("link", { name: "Profile" }).getAttribute("aria-current")).toBe("page");
  expect(screen.getByRole("heading", { level: 1, name: "Profile" })).toBeTruthy();
});

test("admins also find the administration", async () => {
  await settings("/settings/admin/apps", { user: admin });
  expect(nav().getAllByRole("heading").at(-1)!.textContent).toBe("Administration");
  expect(
    nav()
      .getAllByRole("link")
      .slice(-4)
      .map((link) => link.textContent),
  ).toEqual(["Instance", "Users and invites", "Sign-in providers", "Apps"]);
  expect(nav().getAllByRole("link").at(-1)!.getAttribute("aria-current")).toBe("page");
});

test.each([
  ["/settings/personal", "/settings/profile"],
  ["/settings/connections", "/settings/apps"],
  ["/settings/login", "/settings/security"],
])("the old address %s leads to %s", async (old, now) => {
  const app = await settings(old);
  expect(app.path()).toBe(now);
});

test.each([
  ["/settings/admin/invites", "/settings/admin/users"],
  ["/settings/admin/connections", "/settings/admin/apps"],
  ["/settings/admin", "/settings/admin/instance"],
])("the old address %s leads admins to %s", async (old, now) => {
  const app = await settings(old, { user: admin });
  expect(app.path()).toBe(now);
});

test("on a wide screen the settings open on the profile", async () => {
  const app = await settings("/settings");
  expect(app.path()).toBe("/settings/profile");
});

test("an unknown page and the administration for others are not found", async () => {
  await renderApp({ path: "/settings/nope" });
  expect(await screen.findByRole("heading", { name: "Not found" })).toBeTruthy();
  cleanup();
  await renderApp({ path: "/settings/admin/apps" });
  expect(await screen.findByRole("heading", { name: "Not found" })).toBeTruthy();
});

test("on a phone the settings are a list, and every page leads back to it", async () => {
  document.documentElement.style.setProperty("--layout", "narrow");
  const app = await settings("/settings");
  expect(app.path()).toBe("/settings");
  expect(screen.queryByRole("heading", { level: 1, name: "Profile" })).toBeNull();
  await userEvent.click(nav().getByRole("link", { name: "Apps" }));
  expect(app.path()).toBe("/settings/apps");
  expect(screen.queryByRole("navigation", { name: "Settings" })).toBeNull();
  await userEvent.click(screen.getByRole("link", { name: "All settings" }));
  expect(app.path()).toBe("/settings");
});

test("an app whose last reminder failed marks the apps, one that was away does not", async () => {
  const app = await renderApp({ path: "/settings/profile", hold: true });
  app.route("GET", "/users/@me/connections", {
    status: 200,
    body: [
      wireApp({
        id: "a",
        last_delivery: { status: "expired", reason: "", at: "2026-11-12T18:00:00Z", test: false },
      }),
    ],
  });
  await act(() => app.connect());
  await screen.findByRole("navigation", { name: "Settings" });
  expect(nav().queryByRole("img", { name: "An app does not reach you" })).toBeNull();
  cleanup();

  const failing = await renderApp({ path: "/settings/profile", hold: true });
  failing.route("GET", "/users/@me/connections", {
    status: 200,
    body: [
      wireApp({
        id: "a",
        last_delivery: {
          status: "failed",
          reason: "closed",
          at: "2026-11-12T18:00:00Z",
          test: false,
        },
      }),
    ],
  });
  await act(() => failing.connect());
  const link = await within(await screen.findByRole("navigation", { name: "Settings" })).findByRole(
    "link",
    {
      name: /Apps/,
    },
  );
  expect(within(link).getByRole("img", { name: "An app does not reach you" })).toBeTruthy();
});

test("a picture that does not load gives way to initials", () => {
  const { container } = render(<Avatar name="Alex Berger" src="https://hub.test/a.png" />);
  fireEvent.error(container.querySelector("img")!);
  expect(screen.getByText("AB")).toBeTruthy();
  cleanup();
  render(<Avatar name="mia" />);
  expect(screen.getByText("M")).toBeTruthy();
});
