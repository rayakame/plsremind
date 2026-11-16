import { expect, haltHub, openNav, resumeHub, test } from "./helpers.js";

test("the page says when the hub is away and carries on when it is back", async ({
  page,
  account,
}) => {
  await page.goto("/");
  await openNav(page);
  await expect(page.getByRole("link", { name: /Inbox/ })).toBeVisible();
  await haltHub();
  try {
    await expect(page.getByText(/Reconnecting/)).toBeVisible({ timeout: 15_000 });
  } finally {
    await resumeHub();
  }
  await expect(page.getByText(/Reconnecting/)).toBeHidden({ timeout: 30_000 });
  await account.rest.createList({ name: "After the restart" });
  await expect(page.getByRole("link", { name: /After the restart/ })).toBeVisible();
});
