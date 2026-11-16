import { expect, signIn, test } from "./helpers.js";

test("an item is filled in on its page: due time, priority, subitems, a reminder @core", async ({
  page,
  account,
  isMobile,
}) => {
  const created = await account.rest.createItem({ title: "Write the essay" });
  await account.rest.createItem({ title: "Another one" });
  await page.goto("/");
  await page.getByRole("link", { name: "Write the essay" }).click();
  if (!isMobile) {
    const panel = page.getByRole("complementary", { name: "Details" });
    await expect(panel.getByLabel("Title", { exact: true })).toHaveValue("Write the essay");
    await panel.getByRole("button", { name: "Open large" }).click();
  }
  await expect(page).toHaveURL(new RegExp(`/items/${created.id}$`));

  await page.getByLabel("Due date").fill("2030-01-15");
  await page.getByLabel("Due time").fill("12:00");
  await page.getByLabel("Priority").selectOption("High");
  await page.getByLabel("Add subitem").fill("Outline");
  await page.getByLabel("Add subitem").press("Enter");
  await page.getByLabel("Add subitem").fill("First draft");
  await page.getByLabel("Add subitem").press("Enter");
  await page.getByRole("checkbox", { name: "Outline" }).click();
  await page.getByLabel("Remind me").fill("2h");
  await page.getByLabel("Remind me").press("Enter");

  const upcoming = page.getByRole("region", { name: "Upcoming reminders" });
  await expect(upcoming.getByRole("listitem").filter({ hasText: "Set by you" })).toHaveCount(1);
  await expect
    .poll(async () => {
      const item = await account.rest.getItem(created.id);
      return {
        due: item.dueAt?.toISOString(),
        priority: item.priority,
        subitems: item.subitems.map((subitem) => `${subitem.title}:${subitem.done}`),
        reminders: item.reminders.length,
      };
    })
    .toEqual({
      due: "2030-01-15T11:00:00.000Z",
      priority: "high",
      subitems: ["Outline:true", "First draft:false"],
      reminders: 1,
    });

  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Another one");
  await page.getByRole("button", { name: "Previous" }).click();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Write the essay");
  await page.getByRole("button", { name: "Back" }).click();
  await expect(page.getByRole("listitem").filter({ hasText: "Write the essay" })).toContainText(
    "High",
  );
});

test("two tabs of one user stay in step", async ({ page, account, browser }) => {
  const item = await account.rest.createItem({ title: "Shared" });
  const other = await browser.newContext();
  await signIn(other, account);
  const second = await other.newPage();
  await Promise.all([page.goto("/"), second.goto("/")]);

  await second.getByRole("link", { name: "Shared" }).click();
  const details = second.getByRole("complementary", { name: "Details" });
  await expect(details).toBeVisible();

  await page.getByRole("link", { name: "Shared" }).click();
  const title = page.getByRole("complementary", { name: "Details" }).getByLabel("Title");
  await title.fill("Renamed in the first tab");
  await title.press("Enter");
  await expect(second.getByRole("link", { name: "Renamed in the first tab" })).toBeVisible();
  await expect(details.getByLabel("Title", { exact: true })).toHaveValue(
    "Renamed in the first tab",
  );

  await account.rest.deleteItem(item.id);
  await expect(details).toBeHidden();
  await expect(page.getByRole("complementary", { name: "Details" })).toBeHidden();
  await other.close();
});

test("leaving an item goes back to its list, also after Back and Forward", async ({
  page,
  account,
}) => {
  const list = await account.rest.createList({ name: "Errands" });
  await account.rest.createItem({ title: "Buy stamps", listId: list.id });
  await page.goto(`/lists/${list.id}`);
  await page.getByRole("link", { name: "Buy stamps" }).dblclick();
  await expect(page).toHaveURL(/\/items\//);
  await page.getByRole("button", { name: "Back" }).click();
  await expect(page).toHaveURL(new RegExp(`/lists/${list.id}$`));
  await page.goForward();
  await expect(page).toHaveURL(/\/items\//);
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(new RegExp(`/lists/${list.id}$`));
});
