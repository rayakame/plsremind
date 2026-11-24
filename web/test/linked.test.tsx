import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { rememberLinkOrigin } from "../src/pages/linked-origin.js";
import { renderApp, wireApp } from "./support.js";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const info = {
  login_methods: [
    { key: "password", type: "password", name: "Password", login: true, link: false },
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
    { key: "gh", type: "github", name: "GitHub", login: true, link: true },
  ],
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
const install = "https://discord.com/oauth2/authorize?client_id=1";

async function linked(path: string, apps: object[] = [], identities: object[] = [discord]) {
  const app = await renderApp({ path, info, hold: true });
  app.route("GET", "/users/@me/connections", { status: 200, body: apps });
  app.route("GET", "/users/@me/identities", { status: 200, body: identities });
  await act(() => app.connect());
  await screen.findByRole("heading", { level: 1 });
  return app;
}

test("a linked account is confirmed with its picture, name and handle", async () => {
  const app = await linked("/linked/discord?identity=i2", [
    wireApp({ id: "a", name: "plsremind", install_url: install }),
    wireApp({ id: "b", name: "Team" }),
  ]);
  expect(screen.getByRole("heading", { level: 1, name: "Discord is linked" })).toBeTruthy();
  expect(await screen.findByText("You are now linked as Alex Berger (@alexb).")).toBeTruthy();
  expect(document.querySelector(".mark img")!.getAttribute("src")).toBe(
    "http://hub.test/api/v1/users/@me/identities/i2/avatar",
  );
  const next = within(screen.getByRole("region", { name: "What comes next" }));
  expect(next.getByText("plsremind")).toBeTruthy();
  expect(next.getByText("Team")).toBeTruthy();
  expect(next.getAllByRole("link", { name: /Add in Discord/ })).toHaveLength(1);
  expect(screen.queryByRole("navigation", { name: "Navigation" })).toBeNull();
  await userEvent.click(screen.getByRole("link", { name: "Done" }));
  expect(app.path()).toBe("/settings/apps");
});

test("a test reminder goes out through every app of the kind that may remind", async () => {
  const app = await linked("/linked/discord?identity=i2", [
    wireApp({ id: "a", name: "plsremind" }),
    wireApp({ id: "b", name: "Team", remind: false }),
  ]);
  const pending = {
    id: "t1",
    connection_id: "a",
    status: "pending",
    reason: "",
    created_at: "2026-11-12T18:00:00Z",
    closed_at: null,
  };
  app.route("POST", "/users/@me/connections/a/tests", { status: 201, body: pending });
  app.route("GET", "/users/@me/connections/a/tests/t1", {
    status: 200,
    body: {
      ...pending,
      status: "failed",
      reason: "DMs are closed",
      closed_at: "2026-11-12T18:00:01Z",
    },
  });
  await userEvent.click(await screen.findByRole("button", { name: "Send a test reminder" }));
  const row = within(screen.getByRole("listitem", { name: "plsremind" }));
  expect(
    await row.findByText("The test did not arrive: DMs are closed", {}, { timeout: 3000 }),
  ).toBeTruthy();
  expect(app.sent("POST", /\/tests$/).map((call) => call.path)).toEqual([
    "/users/@me/connections/a/tests",
  ]);
});

test("without apps of the kind there is nothing to add, and done leads to the sign-in settings", async () => {
  const app = await linked(
    "/linked/gh?identity=i3",
    [wireApp({ id: "a" })],
    [
      {
        ...discord,
        id: "i3",
        provider: "gh",
        display_name: "Octo Mia",
        username: "",
        has_avatar: false,
      },
    ],
  );
  expect(screen.getByRole("heading", { level: 1, name: "GitHub is linked" })).toBeTruthy();
  expect(await screen.findByText("You are now linked as Octo Mia.")).toBeTruthy();
  expect(screen.queryByRole("region", { name: "What comes next" })).toBeNull();
  await userEvent.click(screen.getByRole("link", { name: "Done" }));
  expect(app.path()).toBe("/settings/security");
});

test("done leads back to where linking started", async () => {
  rememberLinkOrigin("/settings/apps");
  const app = await linked("/linked/gh?identity=i2");
  await userEvent.click(screen.getByRole("link", { name: "Done" }));
  expect(app.path()).toBe("/settings/apps");
});

test.each([
  ["identity_taken", "This Discord account is already linked to another plsremind account."],
  ["link_disabled", "Linking this way is turned off."],
  ["invalid_state", "The link was started in another window or took too long."],
  ["provider_error", "The other service did not answer as expected."],
  ["something_new", "Something went wrong."],
])("the error %s is explained, and nothing was changed", async (code, text) => {
  await linked(`/linked/discord?error=${code}`);
  expect(screen.getByRole("heading", { level: 1, name: "That did not work" })).toBeTruthy();
  expect(screen.getByText(text)).toBeTruthy();
  expect(screen.getByText("Nothing was changed.")).toBeTruthy();
  expect(screen.getByRole("link", { name: "Try again" }).getAttribute("href")).toBe(
    "http://hub.test/api/v1/auth/oauth/discord/start?intent=link",
  );
  expect(screen.getByRole("link", { name: "Back to the settings" })).toBeTruthy();
});

test("a taken account names both ways out", async () => {
  await linked("/linked/discord?error=identity_taken");
  expect(
    screen.getByText(/sign in with the other plsremind account and unlink Discord there/),
  ).toBeTruthy();
  expect(screen.getByText(/sign in to Discord with another account and try again/)).toBeTruthy();
});

test("someone signed out signs in first and comes back", async () => {
  const app = await renderApp({ path: "/linked/discord?identity=i2", user: null, info });
  await waitFor(() => expect(app.path()).toMatch(/^\/login\?next=/));
  expect(decodeURIComponent(app.path())).toContain("/linked/discord?identity=i2");
});

test("the test button waits while its tests run", async () => {
  const app = await linked("/linked/discord?identity=i2", [
    wireApp({ id: "a", name: "plsremind" }),
  ]);
  const pending = {
    id: "t1",
    connection_id: "a",
    status: "pending",
    reason: "",
    created_at: "2026-11-12T18:00:00Z",
    closed_at: null,
  };
  app.route("POST", "/users/@me/connections/a/tests", { status: 201, body: pending });
  app.route("GET", "/users/@me/connections/a/tests/t1", { status: 200, body: pending });
  const button = await screen.findByRole("button", { name: "Send a test reminder" });
  await userEvent.click(button);
  await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(true));
  await userEvent.click(button);
  expect(app.sent("POST", /\/tests$/)).toHaveLength(1);
});

test("trying again remembers where linking started", async () => {
  rememberLinkOrigin("/settings/apps");
  await linked("/linked/discord?error=provider_error");
  const retry = screen.getByRole("link", { name: "Try again" });
  retry.addEventListener("click", (event) => event.preventDefault());
  await userEvent.click(retry);
  expect(sessionStorage.getItem("plsremind.linkFrom")).toMatch(/^\d+ \/settings\/apps$/);
});

test("a garbled address still shows the page", async () => {
  await linked("/linked/%E0%A4%A?error=provider_error");
  expect(screen.getByRole("heading", { level: 1, name: "That did not work" })).toBeTruthy();
});

test("an app that came with the link is shown as added, the others keep their button", async () => {
  await linked("/linked/discord?identity=i2", [
    wireApp({ id: "a", name: "plsremind", install_url: install, installs_with_link: true }),
    wireApp({ id: "b", name: "Team", install_url: install }),
  ]);
  const next = within(screen.getByRole("region", { name: "What comes next" }));
  const own = within(next.getByRole("listitem", { name: "plsremind" }));
  expect(own.getByText("Added with this link")).toBeTruthy();
  expect(own.queryByRole("link")).toBeNull();
  expect(
    within(next.getByRole("listitem", { name: "Team" })).getByRole("link", {
      name: /Add in Discord/,
    }),
  ).toBeTruthy();
  expect(next.getByText(/For an app to write to you/)).toBeTruthy();
});

test("when the link added every app there is nothing left to do", async () => {
  await linked("/linked/discord?identity=i2", [
    wireApp({ id: "a", name: "plsremind", install_url: install, installs_with_link: true }),
  ]);
  const next = within(screen.getByRole("region", { name: "What comes next" }));
  expect(next.getByText("The link also added these apps to your Discord account.")).toBeTruthy();
  expect(next.queryByText(/For an app to write to you/)).toBeNull();
});
