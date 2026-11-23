import { expect, openNav, test } from "./helpers.js";

test("at phone width everything is reachable by touch and nothing scrolls sideways @core", async ({
  page,
  account,
  isMobile,
}) => {
  test.skip(!isMobile, "only at phone width");
  await account.rest.createItem({ title: "Tap me" });
  await account.rest.createList({ name: "Errands" });
  const sideways = () =>
    page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );

  await page.goto("/");
  await expect(page.getByRole("link", { name: "Tap me" })).toBeVisible();
  expect(await sideways()).toBe(0);
  await expect(page.getByRole("link", { name: /Errands/ })).toBeHidden();
  await openNav(page);
  await expect(page.getByRole("link", { name: /Errands/ })).toBeVisible();
  await page.locator(".backdrop").click({ position: { x: 340, y: 300 } });
  await expect(page.getByRole("link", { name: /Errands/ })).toBeHidden();

  await page.getByRole("button", { name: "Create" }).tap();
  const form = page.getByRole("dialog", { name: "New item" });
  await form.getByLabel("Title").fill("Made on the phone");
  await form.getByLabel("Priority").selectOption("High");
  await form.getByRole("button", { name: "Create" }).tap();
  await expect(page.getByRole("listitem")).toHaveCount(2);

  await page.getByRole("link", { name: "Tap me" }).tap();
  await expect(page).toHaveURL(/\/items\//);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Tap me");
  expect(await sideways()).toBe(0);
  await page.getByRole("button", { name: "Complete" }).tap();
  await expect(page.getByRole("button", { name: "Reopen" })).toBeVisible();
  await page.getByRole("button", { name: "Back" }).tap();
  await expect(page.getByRole("listitem")).toHaveCount(1);

  for (const [link, heading] of [
    ["Calendar", /\d{4}/],
    ["Done", "Done"],
    ["Settings", "Settings"],
  ] as const) {
    await openNav(page);
    await page.getByRole("link", { name: link }).click();
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    expect(await sideways(), link).toBe(0);
  }
  await page.goto("/calendar");
  const cell = page.locator("[data-day]").nth(10);
  await cell.tap({ position: { x: 30, y: 30 } });
  await expect(cell.locator(".n")).toHaveAttribute("aria-pressed", "true");
  expect((await cell.boundingBox())!.height).toBeGreaterThanOrEqual(44);

  await page.goto("/settings");
  await page
    .getByRole("navigation", { name: "Settings" })
    .getByRole("link", { name: "Apps" })
    .tap();
  await expect(page.getByRole("heading", { level: 1, name: "Apps" })).toBeVisible();
  expect(await sideways(), "apps").toBe(0);
  await page.getByRole("link", { name: "All settings" }).tap();
  await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();

  await page.goto("/settings/admin");
  await expect(page.getByRole("heading", { name: "Not found" })).toBeVisible();
});
