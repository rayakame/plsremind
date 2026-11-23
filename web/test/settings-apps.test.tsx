import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wireApp } from "./support.js";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const info = {
  login_methods: [
    { key: "password", type: "password", name: "Password", login: true, link: false },
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
  ],
};
const linked = {
  id: "i2",
  provider: "discord",
  external_id: "1234",
  display_name: "Alex Berger",
  username: "alexb",
  has_avatar: true,
  created_at: "2026-11-02T10:00:00Z",
};
const failed = {
  status: "failed",
  reason: "DMs are closed",
  at: "2026-11-12T18:00:00Z",
  test: false,
};
const install = "https://discord.com/oauth2/authorize?client_id=1";

async function apps(list: object[], identities: object[] = [linked]) {
  const app = await renderApp({ path: "/settings/apps", info, hold: true });
  app.route("GET", "/users/@me/connections", { status: 200, body: list });
  app.route("GET", "/users/@me/identities", { status: 200, body: identities });
  await act(() => app.connect());
  await screen.findByRole("heading", { level: 1, name: "Apps" });
  return app;
}

const card = (name: string) => within(screen.getByRole("region", { name }));

test("two Discord apps get two cards with their own state", async () => {
  await apps([
    wireApp({ id: "a", name: "plsremind" }),
    wireApp({ id: "b", name: "Team", remind: false, last_delivery: failed, install_url: install }),
  ]);
  const discord = within(await screen.findByRole("region", { name: "Discord" }));
  expect(discord.getByRole("heading", { name: "Discord" })).toBeTruthy();
  expect(
    within(card("plsremind").getByRole("group"))
      .getByRole("button", { name: "Commands and reminders" })
      .getAttribute("aria-pressed"),
  ).toBe("true");
  expect(
    within(card("Team").getByRole("group"))
      .getByRole("button", { name: "Only commands" })
      .getAttribute("aria-pressed"),
  ).toBe("true");
  expect(card("plsremind").getByText("No reminder through this app yet.")).toBeTruthy();
  expect(
    card("Team").getByText(/^The last reminder did not arrive: DMs are closed This goes away/),
  ).toBeTruthy();
});

test("the Discord account is shown once, above the apps of its kind", async () => {
  await apps([wireApp({ id: "a" })]);
  const account = card("Your Discord account");
  expect(await account.findByText("Alex Berger")).toBeTruthy();
  expect(account.getByText("@alexb · Discord")).toBeTruthy();
  expect(account.getByText("Linked")).toBeTruthy();
  const another = account.getByRole("link", { name: "Link another Discord account" });
  another.addEventListener("click", (event) => event.preventDefault());
  await userEvent.click(another);
  expect(sessionStorage.getItem("plsremind.linkFrom")).toMatch(/^\d+ \/settings\/apps$/);
});

test("without a linked account the card offers to link one and the choice waits for it", async () => {
  await apps([wireApp({ id: "a", linked: false, enabled: false })], []);
  const account = card("Your Discord account");
  expect(await account.findByText("Not linked yet")).toBeTruthy();
  expect(
    account.getByRole("link", { name: "Link your Discord account" }).getAttribute("href"),
  ).toBe("http://hub.test/api/v1/auth/oauth/discord/start?intent=link");
  const choice = within(card("plsremind").getByRole("group"));
  expect((choice.getByRole("button", { name: "Nothing" }) as HTMLButtonElement).disabled).toBe(
    true,
  );
  expect(card("plsremind").getByText("Link your Discord account first to choose.")).toBeTruthy();
});

test("an app without a linkable provider explains and disables the choice", async () => {
  await apps(
    [wireApp({ id: "x", name: "Script", provider: "example", linked: false, enabled: false })],
    [],
  );
  const account = card("Your example account");
  expect(
    await account.findByText("An admin has to turn on linking for example first."),
  ).toBeTruthy();
  expect(account.queryByRole("link")).toBeNull();
  expect(
    within(card("Script").getByRole("group")).getByRole("button", { name: "Access" }),
  ).toBeTruthy();
});

test("the access choice sends what the hub keeps", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  app.route("PUT", "/users/@me/connections/a", (call) => ({
    status: 200,
    body: wireApp({ id: "a", ...(call.body as object) }),
  }));
  const choice = within(card("plsremind").getByRole("group"));
  await userEvent.click(choice.getByRole("button", { name: "Only commands" }));
  await userEvent.click(choice.getByRole("button", { name: "Nothing" }));
  await waitFor(() =>
    expect(app.sent("PUT", "/users/@me/connections/a").map((call) => call.body)).toEqual([
      { enabled: true, remind: false },
      { enabled: false },
    ]),
  );
});

test("the button to add an app in Discord is there with a link, and stands out after a failure", async () => {
  await apps([
    wireApp({ id: "a", name: "plsremind" }),
    wireApp({ id: "b", name: "Team", install_url: install }),
    wireApp({ id: "c", name: "Late", install_url: install, last_delivery: failed }),
  ]);
  expect(card("plsremind").queryByRole("link", { name: /Add in Discord/ })).toBeNull();
  const quiet = card("Team").getByRole("link", { name: /Add in Discord/ });
  expect(quiet.getAttribute("href")).toBe(install);
  expect(quiet.className).not.toContain("primary");
  expect(card("Late").getByRole("link", { name: /Add in Discord/ }).className).toContain("primary");
});

test("a test runs until the app answers, and the status line follows", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  const test = {
    id: "t1",
    connection_id: "a",
    status: "pending",
    reason: "",
    created_at: "2026-11-12T18:00:00Z",
    closed_at: null,
  };
  app.route("POST", "/users/@me/connections/a/tests", { status: 201, body: test });
  app.route("GET", "/users/@me/connections/a/tests/t1", {
    status: 200,
    body: { ...test, status: "delivered", closed_at: "2026-11-12T18:00:01Z" },
  });
  await userEvent.click(card("plsremind").getByRole("button", { name: "Send test" }));
  expect(await card("plsremind").findByText("The test is on its way.")).toBeTruthy();
  expect(
    await card("plsremind").findByText("The test arrived.", {}, { timeout: 3000 }),
  ).toBeTruthy();
  await waitFor(() => expect(app.sent("GET", "/users/@me/connections").length).toBeGreaterThan(1));
});

test("a test that found the app away says so", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  app.route("POST", "/users/@me/connections/a/tests", {
    status: 201,
    body: {
      id: "t1",
      connection_id: "a",
      status: "unanswered",
      reason: "not_connected",
      created_at: "2026-11-12T18:00:00Z",
      closed_at: "2026-11-12T18:00:00Z",
    },
  });
  await userEvent.click(card("plsremind").getByRole("button", { name: "Send test" }));
  expect(
    await card("plsremind").findByText(
      "The app is not connected right now, so the test went nowhere.",
    ),
  ).toBeTruthy();
});

test("a second test within 30 seconds explains the wait", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  app.route("POST", "/users/@me/connections/a/tests", {
    status: 429,
    body: { error: { code: "rate_limited", message: "Wait." } },
  });
  await userEvent.click(card("plsremind").getByRole("button", { name: "Send test" }));
  expect(
    await card("plsremind").findByText("Wait a moment before testing this app again."),
  ).toBeTruthy();
});

test("an instance without apps says so", async () => {
  await apps([]);
  expect(await screen.findByText("No app is set up on this instance yet.")).toBeTruthy();
});

test("a test whose answer cannot be fetched ends with a line that says so", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  const test = {
    id: "t1",
    connection_id: "a",
    status: "pending",
    reason: "",
    created_at: "2026-11-12T18:00:00Z",
    closed_at: null,
  };
  app.route("POST", "/users/@me/connections/a/tests", { status: 201, body: test });
  app.route("GET", "/users/@me/connections/a/tests/t1", {
    status: 500,
    body: { error: { code: "internal", message: "no" } },
  });
  await userEvent.click(card("plsremind").getByRole("button", { name: "Send test" }));
  expect(
    await card("plsremind").findByText(
      "No answer came back. Try the test again.",
      {},
      { timeout: 6000 },
    ),
  ).toBeTruthy();
  expect(
    (card("plsremind").getByRole("button", { name: "Send test" }) as HTMLButtonElement).disabled,
  ).toBe(false);
}, 10_000);

test("a provider that only links does not promise a sign-in", async () => {
  const app = await renderApp({
    path: "/settings/apps",
    info: {
      login_methods: [
        { key: "discord", type: "discord", name: "Discord", login: false, link: true },
      ],
    },
    hold: true,
  });
  app.route("GET", "/users/@me/connections", { status: 200, body: [wireApp({ id: "a" })] });
  app.route("GET", "/users/@me/identities", { status: 200, body: [linked] });
  await act(() => app.connect());
  const account = within(await screen.findByRole("region", { name: "Your Discord account" }));
  expect(
    await account.findByText("Every Discord app of this instance recognises you by it."),
  ).toBeTruthy();
});

test("the account card waits for the accounts and offers to load them again", async () => {
  const app = await renderApp({ path: "/settings/apps", info, hold: true });
  app.route("GET", "/users/@me/connections", { status: 200, body: [wireApp({ id: "a" })] });
  app.route("GET", "/users/@me/identities", {
    status: 500,
    body: { error: { code: "internal", message: "no" } },
  });
  await act(() => app.connect());
  const account = within(await screen.findByRole("region", { name: "Your Discord account" }));
  expect(await account.findByRole("button", { name: "Try again" })).toBeTruthy();
  expect(account.queryByText("Not linked yet")).toBeNull();
});

test("unlinking asks the same way as on the sign-in page", async () => {
  const made = await apps([wireApp({ id: "a" })]);
  made.route("DELETE", "/users/@me/identities/i2", { status: 204 });
  await userEvent.click(
    await card("Your Discord account").findByRole("button", { name: "Unlink" }),
  );
  const dialog = within(screen.getByRole("dialog", { name: "Unlink Discord" }));
  expect(
    dialog.getByText(
      "You can then no longer sign in with it, and apps of this kind no longer recognise you.",
    ),
  ).toBeTruthy();
});

test("the Apps page asks again when it is opened from another settings page", async () => {
  const app = await renderApp({
    path: "/settings/profile",
    info,
    hold: true,
  });
  app.route("GET", "/users/@me/connections", { status: 200, body: [wireApp({ id: "a" })] });
  app.route("GET", "/users/@me/identities", { status: 200, body: [linked] });
  await act(() => app.connect());
  await screen.findByRole("heading", { level: 1, name: "Profile" });
  const before = app.sent("GET", "/users/@me/connections").length;
  await userEvent.click(
    within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", {
      name: "Apps",
    }),
  );
  await waitFor(() =>
    expect(app.sent("GET", "/users/@me/connections").length).toBeGreaterThan(before),
  );
});

test("how an app is doing is announced as it changes", async () => {
  const app = await apps([wireApp({ id: "a" })]);
  app.route("POST", "/users/@me/connections/a/tests", {
    status: 201,
    body: {
      id: "t1",
      connection_id: "a",
      status: "unanswered",
      reason: "no_answer",
      created_at: "2026-11-12T18:00:00Z",
      closed_at: "2026-11-12T18:01:00Z",
    },
  });
  const status = card("plsremind").getByRole("status");
  expect(status.textContent).toBe("No reminder through this app yet.");
  await userEvent.click(card("plsremind").getByRole("button", { name: "Send test" }));
  await waitFor(() => expect(status.textContent).toBe("The app did not answer the test."));
});

test("an app whose kind cannot be linked says why it cannot be chosen", async () => {
  await apps(
    [wireApp({ id: "x", name: "Script", provider: "example", linked: false, enabled: false })],
    [],
  );
  const group = card("Script").getByRole("group");
  const hint = document.getElementById(group.getAttribute("aria-describedby")!)!;
  expect(hint.textContent).toBe("You can choose once an admin turns on linking for example.");
});
