import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp } from "./support.js";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const info = {
  login_methods: [
    { key: "password", type: "password", name: "Password", login: true, link: false },
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
    { key: "gh", type: "github", name: "GitHub", login: true, link: true },
    { key: "idp", type: "oidc", name: "Company", login: false, link: false },
  ],
};
const password = {
  id: "i1",
  provider: "password",
  external_id: "mia",
  display_name: "mia",
  username: "",
  has_avatar: false,
  created_at: "2026-11-01T10:00:00Z",
};
const discord = {
  id: "i2",
  provider: "discord",
  external_id: "1234",
  display_name: "Alex Berger",
  username: "alexb",
  has_avatar: true,
  created_at: "2026-11-02T10:00:00Z",
};

async function security(identities: object[] = [password, discord]) {
  const app = await renderApp({ path: "/settings/security", info, hold: true });
  app.route("GET", "/users/@me/identities", { status: 200, body: identities });
  const { act } = await import("@testing-library/react");
  await act(() => app.connect());
  await screen.findByRole("heading", { level: 1, name: "Sign-in and security" });
  return app;
}

function methods() {
  return within(screen.getByRole("list", { name: "Sign-in methods" }));
}

test("linked accounts show their picture, name, handle and provider", async () => {
  await security();
  const rows = await methods().findAllByRole("listitem");
  expect(rows).toHaveLength(1);
  const row = within(rows[0]!);
  expect(row.getByText("Alex Berger")).toBeTruthy();
  expect(row.getByText("@alexb · Discord")).toBeTruthy();
  expect(row.getByText("Linked")).toBeTruthy();
  expect(rows[0]!.querySelector("img")!.getAttribute("src")).toBe(
    "http://hub.test/api/v1/users/@me/identities/i2/avatar",
  );
});

test("an old identity shows initials and no handle", async () => {
  await security([
    password,
    { ...discord, display_name: "Octo Mia", provider: "gh", username: "", has_avatar: false },
  ]);
  const row = (await methods().findAllByRole("listitem"))[0]!;
  expect(row.querySelector("img")).toBeNull();
  expect(within(row).getByText("OM")).toBeTruthy();
  expect(within(row).getByText("GitHub")).toBeTruthy();
  expect(row.textContent).not.toContain("@");
});

test("only providers that allow linking and are not linked yet are offered", async () => {
  await security();
  await methods().findAllByRole("listitem");
  const link = screen.getByRole("link", { name: "Link GitHub" });
  expect(link.getAttribute("href")).toBe("http://hub.test/api/v1/auth/oauth/gh/start?intent=link");
  expect(screen.queryByRole("link", { name: "Link Discord" })).toBeNull();
  expect(screen.queryByRole("link", { name: "Link Company" })).toBeNull();
  link.addEventListener("click", (event) => event.preventDefault());
  await userEvent.click(link);
  expect(sessionStorage.getItem("plsremind.linkFrom")).toMatch(/^\d+ \/settings\/security$/);
});

test("an account is unlinked after asking", async () => {
  const app = await security();
  app.route("DELETE", "/users/@me/identities/i2", { status: 204 });
  const row = within((await methods().findAllByRole("listitem"))[0]!);
  await userEvent.click(row.getByRole("button", { name: "Unlink" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText("You can then no longer sign in with it.")).toBeTruthy();
  await userEvent.click(dialog.getByRole("button", { name: "Unlink" }));
  await waitFor(() => expect(app.sent("DELETE", "/users/@me/identities/i2")).toHaveLength(1));
  await waitFor(() => expect(screen.queryByRole("listitem")).toBeNull());
});

test("each part of the old personal page has its own page", async () => {
  await renderApp({ path: "/settings/profile" });
  expect(await screen.findByLabelText("Display name")).toBeTruthy();
  expect(screen.queryByText("Colour scheme")).toBeNull();
  cleanup();
  await renderApp({ path: "/settings/look" });
  expect(await screen.findByRole("group", { name: "Colour scheme" })).toBeTruthy();
  expect(screen.queryByLabelText("Display name")).toBeNull();
  cleanup();
  await renderApp({ path: "/settings/keys" });
  expect(await screen.findByLabelText("Command prefix")).toBeTruthy();
  cleanup();
  await renderApp({ path: "/settings/reminders" });
  expect(await screen.findByRole("region", { name: "Quiet hours" })).toBeTruthy();
  expect(screen.queryByLabelText("Command prefix")).toBeNull();
});
