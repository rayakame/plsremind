import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wireUser } from "./support.js";

afterEach(cleanup);

const connection = {
  id: "c1",
  name: "Discord bot",
  description: "Reminds in direct messages",
  provider: "discord",
  linked: true,
  enabled: false,
  remind: true,
  intents: ["users", "items", "deliveries"],
};
const identities = [
  {
    id: "i1",
    provider: "password",
    external_id: "mia",
    display_name: "mia",
    created_at: "2026-11-01T10:00:00Z",
  },
  {
    id: "i2",
    provider: "discord",
    external_id: "123",
    display_name: "mia#1",
    created_at: "2026-11-01T10:00:00Z",
  },
];
const info = {
  login_methods: [
    { key: "password", type: "password", name: "Password", login: true, link: false },
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
    { key: "gh", type: "github", name: "GitHub", login: true, link: true },
  ],
};

async function settings(
  area: string,
  options: Parameters<typeof renderApp>[0] = {},
  first: (app: Awaited<ReturnType<typeof renderApp>>) => void = () => {},
) {
  const app = await renderApp({ path: `/settings/${area}`, info, hold: true, ...options });
  app.route("GET", "/users/@me/connections", {
    status: 200,
    body: [
      connection,
      { ...connection, id: "c2", name: "Mail", provider: "mail", linked: false, enabled: false },
    ],
  });
  app.route("GET", "/users/@me/identities", { status: 200, body: identities });
  app.route("GET", "/users/@me/tokens", {
    status: 200,
    body: [
      {
        id: "t1",
        name: "Laptop",
        remind: false,
        created_at: "2026-11-01T10:00:00Z",
        last_used_at: null,
      },
    ],
  });
  first(app);
  const { act } = await import("@testing-library/react");
  await act(() => app.connect());
  await screen.findByRole("navigation", { name: "Settings" });
  return app;
}

test("a connection is switched on, told to remind, and shows what it gets", async () => {
  const app = await settings("connections");
  app.route("PUT", "/users/@me/connections/c1", (call) => ({
    status: 200,
    body: { ...connection, ...(call.body as object) },
  }));
  const row = within(await screen.findByRole("group", { name: "Discord bot" }));
  expect(row.getByText("Reminds in direct messages")).toBeTruthy();
  expect(row.getByText("Receives: your profile, your items, reminders to pass on")).toBeTruthy();
  await userEvent.click(row.getByRole("checkbox", { name: "Use this connection" }));
  await waitFor(() => expect(app.sent("PUT", "/users/@me/connections/c1")).toHaveLength(1));
  expect(app.sent("PUT", "/users/@me/connections/c1")[0]!.body).toEqual({ enabled: true });
  await waitFor(() =>
    expect(
      (row.getByRole("checkbox", { name: "Use this connection" }) as HTMLInputElement).checked,
    ).toBe(true),
  );
  await userEvent.click(row.getByRole("checkbox", { name: "Remind me here" }));
  await waitFor(() => expect(app.sent("PUT", "/users/@me/connections/c1")).toHaveLength(2));
  expect(app.sent("PUT", "/users/@me/connections/c1")[1]!.body).toEqual({ remind: false });
});

test("names that others chose are shown as text", async () => {
  const markup = '<img src=x onerror="alert(1)">';
  await settings("connections", {}, (made) => {
    made.route("GET", "/users/@me/connections", {
      status: 200,
      body: [{ ...connection, name: markup, description: markup }],
    });
    made.route("GET", "/users/@me/identities", {
      status: 200,
      body: [{ ...identities[1], display_name: markup }],
    });
  });
  await screen.findByRole("group", { name: markup });
  expect(document.querySelector("img")).toBeNull();
});

test("a connection that needs an account elsewhere offers to link it", async () => {
  await settings("connections");
  const row = within(await screen.findByRole("group", { name: "Mail" }));
  expect(row.getByRole("link", { name: "Link your mail account" }).getAttribute("href")).toBe(
    "http://hub.test/api/v1/auth/oauth/mail/start?intent=link",
  );
  expect(
    (row.getByRole("checkbox", { name: "Use this connection" }) as HTMLInputElement).disabled,
  ).toBe(true);
});

test("linked accounts are listed, can be unlinked, and others can be linked", async () => {
  const app = await settings("connections");
  app.route("DELETE", "/users/@me/identities/i2", { status: 204 });
  const accounts = within(await screen.findByRole("region", { name: "Linked accounts" }));
  await waitFor(() => expect(accounts.getAllByRole("listitem")).toHaveLength(2));
  expect(accounts.getAllByRole("listitem").map((row) => row.textContent)).toEqual([
    "Password" + "mia",
    "Discord" + "mia#1" + "Unlink",
  ]);
  expect(accounts.getByRole("link", { name: "Link GitHub" }).getAttribute("href")).toBe(
    "http://hub.test/api/v1/auth/oauth/gh/start?intent=link",
  );
  expect(accounts.queryByRole("link", { name: "Link Discord" })).toBeNull();
  await userEvent.click(accounts.getByRole("button", { name: "Unlink" }));
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Unlink" }));
  await waitFor(() => expect(accounts.getAllByRole("listitem")).toHaveLength(1));
  expect(app.sent("DELETE", "/users/@me/identities/i2")).toHaveLength(1);
});

test("a token is created, shown once, renamed, told to remind and deleted", async () => {
  const app = await settings("tokens");
  app.route("POST", "/users/@me/tokens", {
    status: 201,
    body: {
      id: "t2",
      name: "Phone",
      remind: true,
      token: "plsr_pat_secret",
      created_at: "2026-11-11T10:00:00Z",
      last_used_at: null,
    },
  });
  app.route("PATCH", /tokens\/t1/, (call) => ({
    status: 200,
    body: {
      id: "t1",
      name: "Laptop",
      remind: false,
      created_at: "2026-11-01T10:00:00Z",
      last_used_at: null,
      ...(call.body as object),
    },
  }));
  app.route("DELETE", /tokens\/t1/, { status: 204 });
  await screen.findByRole("group", { name: "Laptop" });

  await userEvent.type(screen.getByLabelText("Name of the new token"), "Phone");
  await userEvent.click(screen.getByRole("checkbox", { name: "Send reminders to this token" }));
  await userEvent.click(screen.getByRole("button", { name: "Create token" }));
  const shown = within(await screen.findByRole("dialog", { name: "Your new token" }));
  expect((shown.getByLabelText("Token") as HTMLInputElement).value).toBe("plsr_pat_secret");
  expect(shown.getByText(/shown only this once/)).toBeTruthy();
  expect(app.sent("POST", "/users/@me/tokens")[0]!.body).toEqual({ name: "Phone", remind: true });
  await userEvent.click(shown.getByRole("button", { name: "Done" }));
  expect(document.body.textContent).not.toContain("plsr_pat_secret");
  expect(document.querySelector('input[value="plsr_pat_secret"]')).toBeNull();
  expect(screen.getByRole("group", { name: "Phone" })).toBeTruthy();

  const laptop = within(screen.getByRole("group", { name: "Laptop" }));
  await userEvent.click(laptop.getByRole("checkbox", { name: "Remind me here" }));
  await waitFor(() => expect(app.sent("PATCH", /tokens\/t1/)).toHaveLength(1));
  expect(app.sent("PATCH", /tokens\/t1/)[0]!.body).toEqual({ remind: true });
  await userEvent.click(laptop.getByRole("button", { name: "Rename" }));
  const name = within(screen.getByRole("dialog")).getByLabelText("Name");
  await userEvent.clear(name);
  await userEvent.type(name, "Work laptop{Enter}");
  await waitFor(() => expect(screen.getByRole("group", { name: "Work laptop" })).toBeTruthy());
  await userEvent.click(
    within(screen.getByRole("group", { name: "Work laptop" })).getByRole("button", {
      name: "Delete",
    }),
  );
  await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.queryByRole("group", { name: "Work laptop" })).toBeNull());
});

test("the password is changed with the current one", async () => {
  const app = await settings("login");
  app.route("PUT", "/users/@me/password", { status: 204 });
  const form = within(await screen.findByRole("region", { name: "Password" }));
  await userEvent.type(form.getByLabelText("Current password"), "old one");
  await userEvent.type(form.getByLabelText("New password"), "a brand new one");
  await userEvent.click(form.getByRole("button", { name: "Change password" }));
  await waitFor(() => expect(app.sent("PUT", "/users/@me/password")).toHaveLength(1));
  expect(app.sent("PUT", "/users/@me/password")[0]!.body).toEqual({
    current_password: "old one",
    new_password: "a brand new one",
  });
  expect(await form.findByText("The password is changed.")).toBeTruthy();
  expect((form.getByLabelText("New password") as HTMLInputElement).value).toBe("");
});

test("an account without a password gets one with a username", async () => {
  const app = await settings("login", {}, (made) =>
    made.route("GET", "/users/@me/identities", { status: 200, body: [identities[1]] }),
  );
  app.route("PUT", "/users/@me/password", {
    status: 409,
    body: { error: { code: "username_taken", message: "Taken." } },
  });
  const form = within(await screen.findByRole("region", { name: "Password" }));
  await userEvent.type(await form.findByLabelText("Username"), "mia");
  await userEvent.type(form.getByLabelText("New password"), "a brand new one");
  await userEvent.click(form.getByRole("button", { name: "Set password" }));
  expect((await form.findByRole("alert")).textContent).toBe("This username is taken.");
  expect(app.sent("PUT", "/users/@me/password")[0]!.body).toEqual({
    username: "mia",
    new_password: "a brand new one",
  });
});

test("deleting the account needs the display name typed out", async () => {
  const app = await settings("login");
  app.route("DELETE", "/users/@me", () => {
    app.setUser(null);
    return { status: 204 };
  });
  await userEvent.click(await screen.findByRole("button", { name: "Delete my account" }));
  const dialog = within(screen.getByRole("dialog"));
  const confirm = dialog.getByRole("button", { name: "Delete for good" }) as HTMLButtonElement;
  expect(confirm.disabled).toBe(true);
  await userEvent.type(dialog.getByLabelText("Type Mia to confirm"), "Mia");
  expect(confirm.disabled).toBe(false);
  await userEvent.click(confirm);
  await waitFor(() => expect(app.sent("DELETE", "/users/@me")).toHaveLength(1));
  await waitFor(() => expect(screen.getByLabelText("Username")).toBeTruthy());
});

test("an error from linking elsewhere is shown and leaves the address", async () => {
  const app = await settings("apps?auth_error=link_disabled");
  expect((await screen.findByRole("alert")).textContent).toBe("Linking this way is turned off.");
  expect(app.path()).toBe("/settings/apps");
});

test("a token made before the list has loaded is not wiped out by the list", async () => {
  const app = await renderApp({ path: "/settings/tokens", info, hold: true });
  const { deferred } = await import("./support.js");
  const listing = deferred<{ status: number; body: unknown }>();
  let listed = 0;
  app.route("GET", "/users/@me/tokens", () =>
    ++listed === 1
      ? listing.promise
      : {
          status: 200,
          body: [
            {
              id: "t2",
              name: "Phone",
              remind: false,
              created_at: "2026-11-11T10:00:00Z",
              last_used_at: null,
            },
          ],
        },
  );
  app.route("POST", "/users/@me/tokens", {
    status: 201,
    body: {
      id: "t2",
      name: "Phone",
      remind: false,
      token: "plsr_pat_secret",
      created_at: "2026-11-11T10:00:00Z",
      last_used_at: null,
    },
  });
  const { act } = await import("@testing-library/react");
  await act(() => app.connect());
  await userEvent.type(await screen.findByLabelText("Name of the new token"), "Phone");
  await userEvent.click(screen.getByRole("button", { name: "Create token" }));
  await userEvent.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Done" }),
  );
  listing.resolve({ status: 200, body: [] });
  await waitFor(() => expect(listed).toBe(2));
  expect(await screen.findByRole("group", { name: "Phone" })).toBeTruthy();
});

test("a list of tokens that did not load can be asked for again", async () => {
  let asked = 0;
  await settings("tokens", {}, (made) =>
    made.route("GET", "/users/@me/tokens", () =>
      ++asked === 1
        ? { status: 500, body: { error: { code: "internal", message: "no" } } }
        : {
            status: 200,
            body: [
              {
                id: "t1",
                name: "Laptop",
                remind: false,
                created_at: "2026-11-01T10:00:00Z",
                last_used_at: null,
              },
            ],
          },
    ),
  );
  await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
  expect(await screen.findByRole("group", { name: "Laptop" })).toBeTruthy();
});
