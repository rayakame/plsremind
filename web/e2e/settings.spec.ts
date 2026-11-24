import { RestClient } from "plsremind";
import {
  admin,
  expect,
  hubUrl,
  openNav,
  restSession,
  seedIdentity,
  test,
  unique,
} from "./helpers.js";

test("language and theme are chosen, apply at once and survive a reload @core", async ({
  page,
  account,
}) => {
  await page.goto("/");
  await openNav(page);
  await page.getByRole("link", { name: "Settings" }).click();
  await page
    .getByRole("navigation", { name: "Settings" })
    .getByRole("link", { name: "Profile" })
    .click();
  await expect(page.getByRole("heading", { level: 1, name: "Profile" })).toBeVisible();
  await page.getByLabel("Language").selectOption("Deutsch");
  await expect(page.getByRole("heading", { level: 1, name: "Profil" })).toBeVisible();
  await expect
    .poll(async () => {
      const user = await account.rest.getUser();
      return "preferences" in user ? user.locale : null;
    })
    .toBe("de");
  await page.goto("/settings/look");
  await page.getByRole("button", { name: "Dunkel" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "Hell", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect
    .poll(async () => {
      const user = await account.rest.getUser();
      return "preferences" in user ? [user.locale, user.preferences.theme] : null;
    })
    .toEqual(["de", "light"]);

  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: "Aussehen" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
});

test("a token made in the settings works against the API @core", async ({ page, account }) => {
  expect(account.user.id).toBeTruthy();
  await page.goto("/settings/tokens");
  await page.getByLabel("Name of the new token").fill("Script");
  await page.getByRole("button", { name: "Create token" }).click();
  const dialog = page.getByRole("dialog", { name: "Your new token" });
  const token = await dialog.getByLabel("Token").inputValue();
  expect(token).toMatch(/^plsr_pat_/);
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("group", { name: "Script" })).toBeVisible();

  const rest = new RestClient({ url: hubUrl, token });
  await rest.createItem({ title: "From a script" });
  await page.goto("/");
  await expect(page.getByRole("link", { name: "From a script" })).toBeVisible();
});

test("quiet hours and a reminder plan are saved @core", async ({ page, account }) => {
  await page.goto("/settings/reminders");
  await page.getByRole("checkbox", { name: "Quiet hours" }).check();
  const high = page.getByRole("region", { name: "High priority" });
  await high.getByRole("button", { name: "Add a time" }).click();
  await high.getByLabel("New time before the deadline").fill("3d");
  await high.getByLabel("New time before the deadline").press("Enter");
  await expect(high.getByRole("listitem").filter({ hasText: "3 days" })).toBeVisible();
  await expect
    .poll(async () => {
      const user = await account.rest.getUser();
      const plans = await account.rest.listReminderPlans();
      const high = plans.find((plan) => plan.priority === "high");
      return [user.quietHours, high?.before.includes(259_200_000)];
    })
    .toEqual([{ start: "22:00", end: "07:00" }, true]);
});

test("every settings page is reached through the side list", async ({ page, account }) => {
  expect(account.user.id).toBeTruthy();
  await page.goto("/settings/profile");
  const pages = [
    "Sign-in and security",
    "When and how often",
    "Apps",
    "Appearance",
    "Keyboard and commands",
    "API tokens",
    "Profile",
  ];
  for (const name of pages) {
    await page.getByRole("navigation", { name: "Settings" }).getByRole("link", { name }).click();
    await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  }
});

test("a failed link explains itself and leads back to where it began @core", async ({
  page,
  account,
}) => {
  expect(account.user.id).toBeTruthy();
  // The dashboard remembers this when a link starts from the sign-in page.
  await page.addInitScript(() => {
    sessionStorage.setItem("plsremind.linkFrom", `${Date.now()} /settings/security`);
  });
  await page.goto("/linked/discord?error=identity_taken");
  await expect(page.getByRole("heading", { level: 1, name: "That did not work" })).toBeVisible();
  await expect(page.getByText("Nothing was changed.")).toBeVisible();
  await page.getByRole("link", { name: "Back to the settings" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Sign-in and security" })).toBeVisible();
});

test("a confirmed link gives each app room for its name and description @core", async ({
  page,
  account,
}) => {
  const session = restSession();
  await session.rest.login(admin);
  const name = unique("Bot");
  await session.rest.createConnection({
    name,
    description: "Reminders as direct messages, and the commands for them",
    provider: "discord",
    installUrl:
      "https://discord.com/oauth2/authorize?client_id=123456789012345678&integration_type=1&scope=applications.commands",
  });
  const identity = await seedIdentity(account.user.id, "discord", unique("ext"));
  await page.goto(`/linked/discord?identity=${identity}`);

  const row = page.getByRole("listitem", { name });
  const title = row.getByText(name, { exact: true });
  const add = row.getByRole("link", { name: /Add in Discord/ });
  await expect(title).toBeVisible();
  const titleBox = (await title.boundingBox())!;
  const addBox = (await add.boundingBox())!;
  const rowBox = (await row.boundingBox())!;
  // One line of text, and the button stays inside the row whether it sits beside or below.
  expect(titleBox.height).toBeLessThan(30);
  expect(titleBox.width).toBeGreaterThan(60);
  expect(addBox.x + addBox.width).toBeLessThanOrEqual(rowBox.x + rowBox.width + 1);
  const description = row.getByText("Reminders as direct messages", { exact: false });
  expect((await description.boundingBox())!.width).toBeGreaterThan(90);
});
