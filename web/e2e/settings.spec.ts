import { RestClient } from "plsremind";
import { expect, hubUrl, openNav, test } from "./helpers.js";

test("language and theme are chosen, apply at once and survive a reload @core", async ({
  page,
  account,
}) => {
  await page.goto("/");
  await openNav(page);
  await page.getByRole("link", { name: "Settings" }).click();
  await page.getByLabel("Language").selectOption("Deutsch");
  await expect(page.getByRole("heading", { level: 1, name: "Einstellungen" })).toBeVisible();
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
  await expect(page.getByRole("heading", { level: 1, name: "Einstellungen" })).toBeVisible();
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

test("quiet hours and a reminder plan are saved", async ({ page, account }) => {
  await page.goto("/settings/personal");
  await page.getByRole("checkbox", { name: "Quiet hours" }).check();
  const high = page.getByRole("group", { name: "High priority" });
  await high.getByLabel("Remind before the deadline").fill("2d, 3h");
  await high.getByLabel("Remind before the deadline").blur();
  await expect
    .poll(async () => {
      const user = await account.rest.getUser();
      const plans = await account.rest.listReminderPlans();
      return [user.quietHours, plans.find((plan) => plan.priority === "high")?.before];
    })
    .toEqual([{ start: "22:00", end: "07:00" }, [172_800_000, 10_800_000]]);
});
