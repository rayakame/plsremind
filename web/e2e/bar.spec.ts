import { expect, test } from "./helpers.js";

test("the bar creates, filters and takes a command @core", async ({ page, account }) => {
  await account.rest.createItem({ title: "Hand in the report" });
  await page.goto("/");
  const bar = page.getByRole("combobox", { name: /Search, create/ });

  await bar.fill("Buy milk");
  await bar.press("Control+Enter");
  await expect(bar).toHaveValue("");
  await expect(page.getByRole("listitem")).toHaveCount(2);

  await bar.fill("milk");
  await expect(page.getByRole("listitem")).toHaveCount(1);
  await expect(page.getByRole("link", { name: "Buy milk" })).toBeVisible();
  await bar.press("Escape");
  await expect(page.getByRole("listitem")).toHaveCount(2);

  await bar.fill("/new Pay the rent tomorrow 12:00 !high");
  await expect(page.getByRole("status", { name: "What was understood" })).toContainText(
    "Pay the rent",
  );
  await bar.press("Enter");
  const row = page.getByRole("listitem").filter({ hasText: "Pay the rent" });
  await expect(row).toContainText("High");
  await expect(row).toContainText("tomorrow 12:00 PM");
  await expect
    .poll(async () => (await account.rest.listItems()).data.map((item) => item.title).sort())
    .toEqual(["Buy milk", "Hand in the report", "Pay the rent"]);

  await bar.fill("/go done");
  await bar.press("Enter");
  await expect(page).toHaveURL(/\/done$/);
});
