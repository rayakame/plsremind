import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import {
  discordApplicationIdOf,
  discordInstallUrl,
  isApplicationId,
} from "../src/pages/settings/admin/apps-model.js";
import { renderApp, wireUser } from "./support.js";

afterEach(cleanup);

const admin = wireUser({ locale: "en", role: "admin" });
const info = {
  login_methods: [
    { key: "password", type: "password", name: "Password", login: true, link: false },
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
    { key: "corp", type: "oidc", name: "Corp SSO", login: true, link: true },
  ],
};
const app = (id: string, name: string, more = {}) => ({
  id,
  name,
  description: "",
  provider: "discord",
  enabled: true,
  allowed_intents: ["users", "lists", "items", "plans", "history", "deliveries"],
  install_url: "",
  reach: 0,
  created_at: "2026-11-01T10:00:00Z",
  last_used_at: null,
  ...more,
});
const id = "123456789012345678";

async function apps(path = "/settings/admin/apps") {
  const made = await renderApp({ path, user: admin, info, hold: true });
  made.route("GET", "/admin/connections", {
    status: 200,
    body: [
      app("b", "Team", { reach: 1, install_url: discordInstallUrl(id) }),
      app("x", "Script", { provider: "corp", install_url: "https://corp.example/setup" }),
      app("a", "plsremind", { reach: 4, last_used_at: "2026-11-10T10:00:00Z" }),
    ],
  });
  await act(() => made.connect());
  await screen.findByRole("heading", { level: 1 });
  return made;
}

test("a Discord install link is made from the Application ID and read back from it", () => {
  expect(isApplicationId(id)).toBe(true);
  expect(isApplicationId("1234")).toBe(false);
  expect(isApplicationId("12345678901234567a")).toBe(false);
  const url = discordInstallUrl(id);
  expect(url).toBe(
    `https://discord.com/oauth2/authorize?client_id=${id}&integration_type=1&scope=applications.commands`,
  );
  expect(discordApplicationIdOf(url)).toBe(id);
  expect(
    discordApplicationIdOf("https://discord.com/oauth2/authorize?client_id=1"),
  ).toBeUndefined();
  expect(
    discordApplicationIdOf("https://example.com/oauth2/authorize?client_id=" + id),
  ).toBeUndefined();
  expect(discordApplicationIdOf("not a link")).toBeUndefined();
});

test("the apps are listed by kind and name with their reach", async () => {
  await apps();
  const rows = await screen.findAllByRole("listitem");
  expect(rows.map((row) => within(row).getByText(/^(plsremind|Team|Script)$/).textContent)).toEqual(
    ["Script", "plsremind", "Team"],
  );
  expect(within(rows[1]!).getByText("Discord")).toBeTruthy();
  expect(within(rows[1]!).getByText(/^Reminds 4 users · last active/)).toBeTruthy();
  expect(within(rows[2]!).getByText("Reminds 1 user · never used")).toBeTruthy();
  expect(within(rows[1]!).getByRole("link", { name: "Edit" }).getAttribute("href")).toBe(
    "/settings/admin/apps/a",
  );
});

test("a new Discord bot is made from its Application ID and shows its token once", async () => {
  const made = await apps();
  await userEvent.click(screen.getByRole("link", { name: "Add Discord bot" }));
  expect(made.path()).toBe("/settings/admin/apps/new/discord");
  made.route("POST", "/admin/connections", (call) => ({
    status: 201,
    body: { ...app("n", "Team"), ...(call.body as object), token: "plsr_con_secret" },
  }));
  await userEvent.type(await screen.findByLabelText("Name"), "Team");
  await userEvent.type(screen.getByLabelText("What users see about it"), "The group's bot");
  const appId = screen.getByLabelText("Application ID from the Discord Developer Portal");
  await userEvent.type(appId, "1234");
  await userEvent.click(screen.getByRole("button", { name: "Add bot and show its token" }));
  expect(screen.getByText("An Application ID has 17 to 20 digits.")).toBeTruthy();
  expect(made.sent("POST", "/admin/connections")).toEqual([]);
  await userEvent.clear(appId);
  await userEvent.type(appId, id);
  await userEvent.click(screen.getByRole("button", { name: "Add bot and show its token" }));
  const shown = within(await screen.findByRole("dialog", { name: "Token of the app" }));
  expect((shown.getByLabelText("Token") as HTMLInputElement).value).toBe("plsr_con_secret");
  expect(shown.getByText(/docs\/self-hosting\.md/)).toBeTruthy();
  expect(made.sent("POST", "/admin/connections")[0]!.body).toEqual({
    name: "Team",
    provider: "discord",
    description: "The group's bot",
    allowed_intents: ["users", "lists", "items", "plans", "history", "deliveries"],
    install_url: discordInstallUrl(id),
  });
  await userEvent.click(shown.getByRole("button", { name: "Done" }));
  expect(made.path()).toBe("/settings/admin/apps");
  expect(document.body.innerHTML).not.toContain("plsr_con_secret");
});

test("another app takes its kind from the providers or typed, and an optional https link", async () => {
  const made = await apps("/settings/admin/apps/new/other");
  made.route("POST", "/admin/connections", (call) => ({
    status: 201,
    body: { ...app("n", "Hook"), ...(call.body as object), token: "plsr_con_x" },
  }));
  await userEvent.type(await screen.findByLabelText("Name"), "Hook");
  await userEvent.type(screen.getByLabelText("Kind"), "corp");
  await userEvent.type(screen.getByLabelText("Where users set it up"), "http://corp.example");
  await userEvent.click(screen.getByRole("button", { name: "Add app and show its token" }));
  expect(screen.getByText("Must be an https address.")).toBeTruthy();
  await userEvent.clear(screen.getByLabelText("Where users set it up"));
  await userEvent.click(screen.getByRole("button", { name: "Add app and show its token" }));
  const shown = await screen.findByRole("dialog");
  expect(shown.textContent).not.toContain("docs/self-hosting.md");
  expect(made.sent("POST", "/admin/connections")[0]!.body).toMatchObject({
    name: "Hook",
    provider: "corp",
  });
  expect(
    Array.from(document.querySelectorAll("datalist option")).map((one) =>
      one.getAttribute("value"),
    ),
  ).toContain("discord");
});

test("an app is edited, gets a new token, is turned off and deleted", async () => {
  const made = await apps("/settings/admin/apps/b");
  made.route("PATCH", "/admin/connections/b", (call) => ({
    status: 200,
    body: { ...app("b", "Team", { install_url: discordInstallUrl(id) }), ...(call.body as object) },
  }));
  made.route("POST", "/admin/connections/b/token", {
    status: 200,
    body: { ...app("b", "Team"), token: "plsr_con_new" },
  });
  made.route("DELETE", "/admin/connections/b", { status: 204 });
  expect(
    (
      (await screen.findByLabelText(
        "Application ID from the Discord Developer Portal",
      )) as HTMLInputElement
    ).value,
  ).toBe(id);
  const name = screen.getByLabelText("Name");
  await userEvent.clear(name);
  await userEvent.type(name, "Team bot");
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(made.sent("PATCH", "/admin/connections/b")).toHaveLength(1));
  expect(made.sent("PATCH", "/admin/connections/b")[0]!.body).toMatchObject({
    name: "Team bot",
    install_url: discordInstallUrl(id),
  });
  await userEvent.click(screen.getByRole("button", { name: "Turn off" }));
  await waitFor(() =>
    expect(made.sent("PATCH", "/admin/connections/b").at(-1)!.body).toEqual({ enabled: false }),
  );
  await userEvent.click(screen.getByRole("button", { name: "New token" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "New token" }),
  );
  const shown = within(await screen.findByRole("dialog"));
  expect((shown.getByLabelText("Token") as HTMLInputElement).value).toBe("plsr_con_new");
  await userEvent.click(shown.getByRole("button", { name: "Done" }));
  await userEvent.click(screen.getByRole("button", { name: "Delete" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(made.path()).toBe("/settings/admin/apps"));
});

test("an app with a link of another form shows the link itself", async () => {
  await apps("/settings/admin/apps/x");
  expect(((await screen.findByLabelText("Where users set it up")) as HTMLInputElement).value).toBe(
    "https://corp.example/setup",
  );
  expect(screen.queryByLabelText("Application ID from the Discord Developer Portal")).toBeNull();
});

test("a list of apps that did not load can be asked for again", async () => {
  const made = await renderApp({ path: "/settings/admin/apps", user: admin, info, hold: true });
  made.route("GET", "/admin/connections", {
    status: 500,
    body: { error: { code: "internal", message: "no" } },
  });
  await act(() => made.connect());
  expect(await screen.findByRole("button", { name: "Try again" })).toBeTruthy();
});

test("a Discord bot whose link was set by hand keeps it when saved twice", async () => {
  const made = await renderApp({ path: "/settings/admin/apps/c", user: admin, info, hold: true });
  made.route("GET", "/admin/connections", {
    status: 200,
    body: [app("c", "Custom", { install_url: "https://example.com/add" })],
  });
  made.route("PATCH", "/admin/connections/c", (call) => ({
    status: 200,
    body: { ...app("c", "Custom"), ...(call.body as object) },
  }));
  await act(() => made.connect());
  const link = await screen.findByLabelText("Where users set it up");
  await userEvent.clear(link);
  await userEvent.type(link, discordInstallUrl(id));
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(made.sent("PATCH", "/admin/connections/c")).toHaveLength(1));
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(made.sent("PATCH", "/admin/connections/c")).toHaveLength(2));
  expect(made.sent("PATCH", "/admin/connections/c")[1]!.body).toMatchObject({
    install_url: discordInstallUrl(id),
  });
});

test("an app added while the list did not load brings the whole list", async () => {
  const made = await renderApp({
    path: "/settings/admin/apps/new/other",
    user: admin,
    info,
    hold: true,
  });
  made.route("GET", "/admin/connections", {
    status: 500,
    body: { error: { code: "internal", message: "no" } },
  });
  made.route("POST", "/admin/connections", (call) => ({
    status: 201,
    body: { ...app("n", "Hook"), ...(call.body as object), token: "plsr_con_x" },
  }));
  await act(() => made.connect());
  await userEvent.type(await screen.findByLabelText("Name"), "Hook");
  await userEvent.type(screen.getByLabelText("Kind"), "corp");
  made.route("GET", "/admin/connections", {
    status: 200,
    body: [app("t", "Teamwork"), app("n", "Hook", { provider: "corp" })],
  });
  await userEvent.click(screen.getByRole("button", { name: "Add app and show its token" }));
  await userEvent.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Done" }),
  );
  expect(await screen.findByText("Teamwork")).toBeTruthy();
});
