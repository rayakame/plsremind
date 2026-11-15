import { expect, openNav, seedDone, test } from "./helpers.js";

test("completed items show at once, older ones load on demand, and one is reopened", async ({
  page,
  account,
}) => {
  await seedDone(account.user.id, 1030);
  await page.goto("/");
  await openNav(page);
  await page.getByRole("link", { name: "Done" }).click();
  await expect(page.getByRole("link", { name: "Done 1", exact: true })).toBeVisible();
  const rows = await page.getByRole("listitem").count();
  expect(rows).toBeLessThan(100);

  const list = page.locator(".scroll");
  await list.evaluate((element) => element.scrollTo(0, element.scrollHeight));
  await expect(page.getByRole("link", { name: "Done 1000", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Done 1001", exact: true })).toHaveCount(0);

  await page.getByRole("button", { name: "Load older" }).click();
  await expect(page.getByRole("button", { name: "Load older" })).toBeHidden();
  await list.evaluate((element) => element.scrollTo(0, element.scrollHeight));
  await expect(page.getByRole("link", { name: "Done 1030", exact: true })).toBeVisible();

  await page.getByRole("checkbox", { name: "Reopen Done 1030" }).click();
  await expect(page.getByRole("link", { name: "Done 1030", exact: true })).toHaveCount(0);
  await expect
    .poll(async () => (await account.rest.listItems()).data.map((item) => item.title))
    .toEqual(["Done 1030"]);
  await openNav(page);
  await expect(page.getByRole("link", { name: /All items/ })).toContainText("1");
});
