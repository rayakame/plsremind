import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wireUser } from "./support.js";

afterEach(cleanup);

const admin = wireUser({ locale: "en", role: "admin" });
const adminUser = (id: string, name: string, more = {}) => ({
  id,
  display_name: name,
  role: "user",
  timezone: "Europe/Berlin",
  quiet_hours: null,
  locale: null,
  created_at: "2026-11-01T10:00:00Z",
  disabled: false,
  ...more,
});
const provider = (key: string, more = {}) => ({
  key,
  type: key,
  name: key,
  login_enabled: false,
  link_enabled: false,
  client_id: "",
  has_client_secret: false,
  issuer_url: "",
  ...more,
});
const connection = {
  id: "c1",
  name: "Discord bot",
  description: "",
  provider: "discord",
  enabled: true,
  allowed_intents: ["users", "items"],
  created_at: "2026-11-01T10:00:00Z",
  last_used_at: null,
};

async function page(
  section: string,
  first: (app: Awaited<ReturnType<typeof renderApp>>) => void = () => {},
) {
  const app = await renderApp({ path: `/settings/admin/${section}`, user: admin, hold: true });
  app.route("GET", "/admin/settings", {
    status: 200,
    body: { name: "plsremind", registration: "open" },
  });
  app.route("GET", "/admin/users", (call) =>
    call.path.includes("cursor=u2")
      ? { status: 200, body: { data: [adminUser("u3", "Cleo")], next_cursor: null } }
      : {
          status: 200,
          body: {
            data: [adminUser("u1", "Mia", { role: "admin" }), adminUser("u2", "Ben")],
            next_cursor: "u2",
          },
        },
  );
  app.route("GET", "/admin/invites", {
    status: 200,
    body: [
      {
        id: "v1",
        created_at: "2026-11-01T10:00:00Z",
        expires_at: "2099-01-01T00:00:00Z",
        used_at: null,
        used_by: null,
      },
      {
        id: "v2",
        created_at: "2026-11-01T10:00:00Z",
        expires_at: "2099-01-01T00:00:00Z",
        used_at: "2026-11-02T10:00:00Z",
        used_by: "u2",
      },
      {
        id: "v3",
        created_at: "2020-01-01T10:00:00Z",
        expires_at: "2020-01-08T10:00:00Z",
        used_at: null,
        used_by: null,
      },
    ],
  });
  app.route("GET", "/admin/providers", {
    status: 200,
    body: [
      provider("password", { name: "Password", login_enabled: true }),
      provider("github", { name: "GitHub" }),
      provider("corp", {
        type: "oidc",
        name: "Corp SSO",
        client_id: "abc",
        has_client_secret: true,
        issuer_url: "https://sso.example",
      }),
    ],
  });
  app.route("GET", "/admin/connections", { status: 200, body: [connection] });
  first(app);
  await act(() => app.connect());
  await screen.findByRole("navigation", { name: "Administration" });
  return app;
}

test("administration does not exist for someone who is not an admin", async () => {
  const app = await renderApp({ path: "/settings/admin/users" });
  expect(await screen.findByRole("heading", { name: "Not found" })).toBeTruthy();
  expect(app.calls.filter((call) => call.path.startsWith("/admin"))).toEqual([]);
  expect(screen.queryByRole("link", { name: "Administration" })).toBeNull();
});

test("the instance gets a name and a way to register", async () => {
  const app = await page("instance");
  app.route("PATCH", "/admin/settings", (call) => ({
    status: 200,
    body: { name: "plsremind", registration: "open", ...(call.body as object) },
  }));
  const name = await screen.findByLabelText("Name of this instance");
  await userEvent.clear(name);
  await userEvent.type(name, "Family todo");
  await userEvent.tab();
  await userEvent.click(screen.getByRole("button", { name: "By invite only" }));
  await waitFor(() => expect(app.sent("PATCH", "/admin/settings")).toHaveLength(2));
  expect(app.sent("PATCH", "/admin/settings").map((call) => call.body)).toEqual([
    { name: "Family todo" },
    { registration: "invite" },
  ]);
});

test("users are promoted, blocked, given a new password and deleted", async () => {
  const app = await page("users");
  app.route("PATCH", "/admin/users/u2", (call) => ({
    status: 200,
    body: adminUser("u2", "Ben", call.body as object),
  }));
  app.route("PUT", "/admin/users/u2/password", { status: 204 });
  app.route("DELETE", "/admin/users/u2", { status: 204 });
  const ben = within(await screen.findByRole("group", { name: "Ben" }));
  await userEvent.selectOptions(ben.getByLabelText("Role"), "Admin");
  await waitFor(() => expect(app.sent("PATCH", "/admin/users/u2")).toHaveLength(1));
  await userEvent.click(ben.getByRole("checkbox", { name: "Blocked" }));
  await waitFor(() => expect(app.sent("PATCH", "/admin/users/u2")).toHaveLength(2));
  expect(app.sent("PATCH", "/admin/users/u2").map((call) => call.body)).toEqual([
    { role: "admin" },
    { disabled: true },
  ]);

  await userEvent.click(ben.getByRole("button", { name: "New password" }));
  const dialog = within(screen.getByRole("dialog"));
  await userEvent.type(dialog.getByLabelText("New password"), "a fresh password");
  await userEvent.click(dialog.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(app.sent("PUT", "/admin/users/u2/password")).toHaveLength(1));
  expect(app.sent("PUT", "/admin/users/u2/password")[0]!.body).toEqual({
    new_password: "a fresh password",
  });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(document.body.innerHTML).not.toContain("a fresh password");

  await userEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(await screen.findByRole("group", { name: "Cleo" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();

  await userEvent.click(ben.getByRole("button", { name: "Delete" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.queryByRole("group", { name: "Ben" })).toBeNull());
});

test("an invite is created, its code shown once, and its state listed", async () => {
  const app = await page("invites");
  app.route("POST", "/admin/invites", {
    status: 201,
    body: {
      id: "v9",
      code: "INV-123",
      created_at: "2026-11-11T10:00:00Z",
      expires_at: "2099-01-01T00:00:00Z",
      used_at: null,
      used_by: null,
    },
  });
  app.route("DELETE", "/admin/invites/v1", { status: 204 });
  await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(3));
  expect(
    screen
      .getAllByRole("listitem")
      .map((row) => within(row).getByText(/Open|Used|Expired/).textContent),
  ).toEqual(["Open", "Used", "Expired"]);
  await userEvent.selectOptions(screen.getByLabelText("Valid for"), "30 days");
  await userEvent.click(screen.getByRole("button", { name: "Create invite" }));
  const shown = within(await screen.findByRole("dialog", { name: "New invite" }));
  expect((shown.getByLabelText("Invite code") as HTMLInputElement).value).toBe("INV-123");
  expect(app.sent("POST", "/admin/invites")[0]!.body).toEqual({ expires_in: "30d" });
  await userEvent.click(shown.getByRole("button", { name: "Done" }));
  expect(document.body.innerHTML).not.toContain("INV-123");
  expect(screen.getAllByRole("listitem")).toHaveLength(4);
  await userEvent.click(screen.getAllByRole("button", { name: "Delete" })[0]!);
  await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(3));
});

test("a provider is configured without its secret ever coming back", async () => {
  const app = await page("providers");
  app.route("PUT", /admin\/providers\//, (call) => ({
    status: 200,
    body: provider(call.path.split("/").at(-1)!, {
      type: "oidc",
      ...(call.body as Record<string, unknown>),
      client_secret: undefined,
      has_client_secret: true,
    }),
  }));
  app.route("DELETE", "/admin/providers/corp", { status: 204 });
  const github = within(await screen.findByRole("group", { name: "GitHub" }));
  await userEvent.type(github.getByLabelText("Client ID"), "gh-id");
  await userEvent.type(github.getByLabelText("Client secret"), "gh-secret");
  await userEvent.click(github.getByRole("checkbox", { name: "Sign in" }));
  await userEvent.click(github.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(app.sent("PUT", "/admin/providers/github")).toHaveLength(1));
  expect(app.sent("PUT", "/admin/providers/github")[0]!.body).toEqual({
    name: "GitHub",
    login_enabled: true,
    link_enabled: false,
    client_id: "gh-id",
    client_secret: "gh-secret",
  });
  await waitFor(() => expect(document.body.innerHTML).not.toContain("gh-secret"));

  const corp = within(screen.getByRole("group", { name: "Corp SSO" }));
  expect((corp.getByLabelText("Client secret") as HTMLInputElement).placeholder).toBe(
    "Stored. Leave empty to keep it.",
  );
  await userEvent.click(corp.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(app.sent("PUT", "/admin/providers/corp")).toHaveLength(1));
  expect(app.sent("PUT", "/admin/providers/corp")[0]!.body).not.toHaveProperty("client_secret");
  expect(
    within(screen.getByRole("group", { name: "Password" })).queryByLabelText("Client ID"),
  ).toBeNull();

  await userEvent.type(screen.getByLabelText("Key of the new provider"), "school");
  await userEvent.click(screen.getByRole("button", { name: "Add provider" }));
  await waitFor(() => expect(app.sent("PUT", "/admin/providers/school")).toHaveLength(1));
  expect(app.sent("PUT", "/admin/providers/school")[0]!.body).toEqual({
    type: "oidc",
    name: "school",
  });

  await userEvent.click(corp.getByRole("button", { name: "Delete" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.queryByRole("group", { name: "Corp SSO" })).toBeNull());
});

test("a connection is installed, limited, given a new token and removed", async () => {
  const app = await page("connections");
  app.route("POST", "/admin/connections", {
    status: 201,
    body: {
      ...connection,
      id: "c2",
      name: "Mail",
      provider: "mail",
      allowed_intents: ["deliveries"],
      token: "plsr_con_secret",
    },
  });
  app.route("PATCH", "/admin/connections/c1", (call) => ({
    status: 200,
    body: { ...connection, ...(call.body as object) },
  }));
  app.route("POST", "/admin/connections/c1/token", {
    status: 200,
    body: { ...connection, token: "plsr_con_rotated" },
  });
  app.route("DELETE", "/admin/connections/c1", { status: 204 });

  const bot = within(await screen.findByRole("group", { name: "Discord bot" }));
  await userEvent.click(bot.getByRole("checkbox", { name: "reminders to pass on" }));
  await waitFor(() => expect(app.sent("PATCH", "/admin/connections/c1")).toHaveLength(1));
  expect(app.sent("PATCH", "/admin/connections/c1")[0]!.body).toEqual({
    allowed_intents: ["users", "items", "deliveries"],
  });
  await userEvent.click(bot.getByRole("checkbox", { name: "Enabled" }));
  await waitFor(() => expect(app.sent("PATCH", "/admin/connections/c1")).toHaveLength(2));
  expect(app.sent("PATCH", "/admin/connections/c1")[1]!.body).toEqual({ enabled: false });

  await userEvent.click(bot.getByRole("button", { name: "New token" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "New token" }),
  );
  const rotated = within(await screen.findByRole("dialog", { name: "Token of the connection" }));
  expect((rotated.getByLabelText("Token") as HTMLInputElement).value).toBe("plsr_con_rotated");
  await userEvent.click(rotated.getByRole("button", { name: "Done" }));
  expect(document.body.innerHTML).not.toContain("plsr_con_rotated");

  const form = within(screen.getByRole("region", { name: "Install a connection" }));
  await userEvent.type(form.getByLabelText("Name"), "Mail");
  await userEvent.type(form.getByLabelText("Provider"), "mail");
  await userEvent.click(form.getByRole("checkbox", { name: "reminders to pass on" }));
  await userEvent.click(form.getByRole("button", { name: "Install" }));
  const made = within(await screen.findByRole("dialog", { name: "Token of the connection" }));
  expect((made.getByLabelText("Token") as HTMLInputElement).value).toBe("plsr_con_secret");
  expect(app.sent("POST", "/admin/connections")[0]!.body).toEqual({
    name: "Mail",
    provider: "mail",
    allowed_intents: ["deliveries"],
  });
  await userEvent.click(made.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("group", { name: "Mail" })).toBeTruthy();

  await userEvent.click(bot.getByRole("button", { name: "Delete" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.queryByRole("group", { name: "Discord bot" })).toBeNull());
});

test("a name the hub refuses falls back to the stored one", async () => {
  const app = await page("instance");
  app.route("PATCH", "/admin/settings", {
    status: 422,
    body: { error: { code: "validation_failed", message: "no", fields: { name: "is too long" } } },
  });
  const name = (await screen.findByLabelText("Name of this instance")) as HTMLInputElement;
  await userEvent.clear(name);
  await userEvent.type(name, "Something else");
  await userEvent.tab();
  expect(await screen.findByRole("alert")).toBeTruthy();
  await waitFor(() => expect(name.value).toBe("plsremind"));
});

test("the password provider can be allowed for linking, without credentials of its own", async () => {
  const app = await page("providers");
  app.route("PUT", "/admin/providers/password", (call) => ({
    status: 200,
    body: provider("password", { name: "Password", ...(call.body as object) }),
  }));
  const password = within(await screen.findByRole("group", { name: "Password" }));
  await userEvent.click(password.getByRole("checkbox", { name: "Link accounts" }));
  await userEvent.click(password.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(app.sent("PUT", "/admin/providers/password")).toHaveLength(1));
  expect(app.sent("PUT", "/admin/providers/password")[0]!.body).toEqual({
    name: "Password",
    login_enabled: true,
    link_enabled: true,
  });
});
