import { expect, openNav, test } from "./helpers.js";

test("the calendar shows items on their day and creates on a chosen one @core", async ({
  page,
  account,
  problems,
}) => {
  await page.goto("/");
  await openNav(page);
  await page.getByRole("link", { name: "Calendar" }).click();
  const today = page.locator("[data-day]").filter({ has: page.locator('[aria-current="date"]') });
  const day = await today.getAttribute("data-day");
  await today.locator(".n").click();
  await expect(today.locator(".n")).toHaveAttribute("aria-pressed", "true");

  const bar = page.getByRole("combobox", { name: /Search, create/ });
  await bar.fill("Dentist");
  await bar.press("Control+Enter");
  await expect(page.getByRole("button", { name: "Dentist" })).toBeVisible();
  await expect
    .poll(async () => (await account.rest.listItems()).data.map((item) => item.dueDate))
    .toEqual([day]);

  await page.getByRole("button", { name: "Week" }).click();
  await expect(page.locator("[data-day]")).toHaveCount(7);
  await expect(page.getByRole("button", { name: "Dentist" })).toBeVisible();
  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByRole("button", { name: "Dentist" })).toBeHidden();
  expect(problems).toEqual([]);
});
