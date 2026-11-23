import { audit } from "./audit.js";
import {
  address,
  admin,
  expect,
  hubUrl,
  restSession,
  signIn,
  test,
  unique,
  watch,
} from "./helpers.js";
import { proxyHeader } from "../../sdk/ts/test/hub/launch.js";

test("an admin invites someone, sees them register, and blocks them", async ({ browser }) => {
  const session = restSession();
  const user = await session.rest.login(admin);
  const adminContext = await browser.newContext({ baseURL: hubUrl });
  await adminContext.setExtraHTTPHeaders({ [proxyHeader]: address() });
  await signIn(adminContext, {
    rest: session.rest,
    user,
    username: admin.username,
    password: admin.password,
    cookie: session.cookie(),
  });
  const page = await adminContext.newPage();
  const problems: string[] = [];
  watch(page, problems);

  for (const section of ["instance", "providers", "apps", "apps/new/discord", "users"]) {
    await page.goto(`/settings/admin/${section}`);
    await expect(page.getByRole("navigation", { name: "Settings" })).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(await audit(page), section).toEqual([]);
  }

  await page.getByRole("button", { name: "Create invite" }).click();
  const code = await page.getByRole("dialog").getByLabel("Invite code").inputValue();
  await page.getByRole("dialog").getByRole("button", { name: "Done" }).click();
  await session.rest.updateSettings({ registration: "invite" });

  try {
    const guestContext = await browser.newContext({ baseURL: hubUrl });
    await guestContext.setExtraHTTPHeaders({ [proxyHeader]: address() });
    const guest = await guestContext.newPage();
    const username = unique("guest");
    await guest.goto("/register");
    await guest.getByLabel("Invite code").fill(code);
    await guest.getByLabel("Username").fill(username);
    await guest.getByLabel("Password").fill("a password for the guest");
    await guest.getByRole("button", { name: "Create account" }).click();
    await expect(guest.getByRole("heading", { level: 1, name: "All items" })).toBeVisible();

    await page.goto("/settings/admin/users");
    await expect(page.getByRole("group").first()).toBeVisible();
    const more = page.getByRole("button", { name: "Load more" });
    const users = page.locator("fieldset.card");
    while (!(await page.getByRole("group", { name: username }).isVisible())) {
      const before = await users.count();
      await more.click();
      await expect.poll(() => users.count()).toBeGreaterThan(before);
    }
    await page
      .getByRole("group", { name: username })
      .getByRole("checkbox", { name: "Blocked" })
      .click();
    await expect(guest.getByLabel("Username")).toBeVisible({ timeout: 15_000 });
    await guestContext.close();
  } finally {
    await session.rest.updateSettings({ registration: "open" });
  }
  expect(problems).toEqual([]);
  await adminContext.close();
});

test("the focus stays in the dialog while a new password is on its way", async ({ browser }) => {
  const session = restSession();
  const user = await session.rest.login(admin);
  const context = await browser.newContext({ baseURL: hubUrl });
  await context.setExtraHTTPHeaders({ [proxyHeader]: address() });
  await signIn(context, {
    rest: session.rest,
    user,
    username: admin.username,
    password: admin.password,
    cookie: session.cookie(),
  });
  const page = await context.newPage();
  let held: (() => Promise<void>) | undefined;
  await page.route("**/admin/users/*/password", (route) => {
    held = () => route.abort();
  });
  await page.goto("/settings/admin/users");
  await page.getByRole("button", { name: "New password" }).first().click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("New password").fill("never sent to the hub");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByRole("button", { name: "Save" })).toBeDisabled();
  const inside = () =>
    page.evaluate(() => {
      const active = document.activeElement;
      return !!active?.closest('[role="dialog"]') && !active.matches(":disabled");
    });
  expect(await inside()).toBe(true);
  await page.keyboard.press("Tab");
  await page.keyboard.press("Tab");
  expect(await inside()).toBe(true);
  await held?.();
  await context.close();
});

test("apps of any kind are added and every user sees them grouped", async ({ browser }) => {
  const session = restSession();
  const user = await session.rest.login(admin);
  const context = await browser.newContext({ baseURL: hubUrl });
  await context.setExtraHTTPHeaders({ [proxyHeader]: address() });
  await signIn(context, {
    rest: session.rest,
    user,
    username: admin.username,
    password: admin.password,
    cookie: session.cookie(),
  });
  const page = await context.newPage();
  const bot = unique("Bot");
  const hook = unique("Hook");

  await page.goto("/settings/admin/apps");
  await page.getByRole("link", { name: "Add Discord bot" }).click();
  await page.getByLabel("Name").fill(bot);
  await page
    .getByLabel("Application ID from the Discord Developer Portal")
    .fill("123456789012345678");
  await page.getByRole("button", { name: "Add bot and show its token" }).click();
  await expect(page.getByRole("dialog").getByLabel("Token")).toHaveValue(/^plsr_con_/);
  await page.getByRole("dialog").getByRole("button", { name: "Done" }).click();

  await page.getByRole("link", { name: "Add another app" }).click();
  await page.getByLabel("Name").fill(hook);
  await page.getByLabel("Kind").fill("example");
  await page.getByRole("button", { name: "Add app and show its token" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("listitem").filter({ hasText: bot })).toBeVisible();

  await page
    .getByRole("navigation", { name: "Settings" })
    .getByRole("link", { name: "Apps", exact: true })
    .first()
    .click();
  // This instance has no Discord sign-in, so the section is named after the key.
  const discord = page.getByRole("region", { name: "discord", exact: true });
  await expect(discord.getByRole("region", { name: bot })).toBeVisible();
  await expect(
    discord.getByRole("region", { name: bot }).getByRole("link", { name: /Add in Discord/ }),
  ).toHaveAttribute("href", /client_id=123456789012345678/);
  await expect(
    page.getByRole("region", { name: "example", exact: true }).getByRole("region", { name: hook }),
  ).toBeVisible();
  await context.close();
});
