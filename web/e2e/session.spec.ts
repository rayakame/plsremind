import { expect, openNav, test, unique } from "./helpers.js";

test("someone registers, stays signed in over a reload, signs out and in again @core", async ({
  page,
  problems,
}) => {
  const username = unique("zoe");
  await page.goto("/calendar");
  await expect(page).toHaveURL(/\/login\?next=%2Fcalendar$/);
  await page.getByRole("link", { name: "Create an account" }).click();
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill("a password for the test");
  await page.getByRole("button", { name: "Create account" }).click();

  await openNav(page);
  await expect(page.getByRole("link", { name: /Inbox/ })).toBeVisible();

  await page.reload();
  await openNav(page);
  await expect(page.getByRole("link", { name: /Inbox/ })).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByLabel("Username")).toBeVisible();

  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill("wrong");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert")).toHaveText("Wrong username or password.");
  await page.getByLabel("Password").fill("a password for the test");
  await page.getByRole("button", { name: "Sign in" }).click();
  await openNav(page);
  await expect(page.getByRole("link", { name: /Inbox/ })).toBeVisible();
  expect(problems.filter((problem) => !problem.includes("401"))).toEqual([]);
});

test("a signed-in user sees their lists at once @core", async ({ page, account }) => {
  await account.rest.createList({ name: "Errands" });
  await page.goto("/");
  await openNav(page);
  await expect(page.getByRole("link", { name: /Errands/ })).toBeVisible();
});
