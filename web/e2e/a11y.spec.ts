import { audit } from "./audit.js";
import { expect, test } from "./helpers.js";

for (const scheme of ["light", "dark"] as const) {
  test(`every page is readable and named in ${scheme}`, async ({ page, account }) => {
    await page.emulateMedia({ colorScheme: scheme });
    const list = await account.rest.createList({ name: "Work" });
    const item = await account.rest.createItem({
      title: "Overdue and urgent",
      listId: list.id,
      priority: "high",
      dueAt: new Date(Date.now() - 86_400_000),
    });
    await account.rest.createItem({ title: "Later", dueAt: new Date(Date.now() + 86_400_000) });
    const done = await account.rest.createItem({ title: "Finished" });
    await account.rest.updateItem(done.id, { status: "done" });
    await account.rest.createSubitem(item.id, { title: "A step" });

    const paths = [
      "/",
      `/lists/${list.id}`,
      "/calendar",
      "/done",
      `/items/${item.id}`,
      "/settings/personal",
      "/settings/connections",
      "/settings/tokens",
      "/settings/login",
      "/find?q=later",
    ];
    for (const path of paths) {
      await page.goto(path);
      await expect(
        page
          .getByRole("heading", { level: 1 })
          .or(page.getByLabel("Title", { exact: true }))
          .first(),
      ).toBeVisible();
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      expect(await audit(page), `${path} in ${scheme}`).toEqual([]);
    }

    await page.goto("/");
    await page.getByRole("link", { name: "Overdue and urgent" }).click();
    await expect(page.getByRole("complementary", { name: "Details" })).toBeVisible();
    expect(await audit(page), `details in ${scheme}`).toEqual([]);
    await page.getByRole("combobox", { name: /Search, create/ }).fill("/");
    await expect(page.getByRole("listbox")).toBeVisible();
    expect(await audit(page), `commands in ${scheme}`).toEqual([]);
    await page.keyboard.press("Escape");
    await page.keyboard.press("?");
    await expect(page.getByRole("dialog", { name: "Keys" })).toBeVisible();
    expect(await audit(page), `dialog in ${scheme}`).toEqual([]);
  });
}

test("the pages before signing in are readable and named", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    for (const path of ["/login", "/register"]) {
      await page.goto(path);
      await expect(page.getByLabel("Username")).toBeVisible();
      expect(await audit(page), `${path} in ${scheme}`).toEqual([]);
    }
  }
});

test("an item is created, selected, opened and completed with the keyboard alone", async ({
  page,
  account,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "All items" })).toBeVisible();
  await page.keyboard.press("n");
  await page.keyboard.type("Typed without a mouse tomorrow");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("listitem")).toHaveCount(1);
  await page.keyboard.press("Escape");
  await page.keyboard.press("j");
  await expect(page.getByRole("complementary", { name: "Details" })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/items\//);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed without a mouse");

  await page.goBack();
  let reached = false;
  for (let i = 0; i < 40 && !reached; i++) {
    await page.keyboard.press("Tab");
    reached = await page.evaluate(
      () => document.activeElement?.getAttribute("aria-label") === "Complete Typed without a mouse",
    );
  }
  expect(reached).toBe(true);
  await page.keyboard.press("Space");
  await expect(page.getByRole("listitem")).toHaveCount(0);
  await expect
    .poll(async () => (await account.rest.listItems({ status: "done" })).data.map((i) => i.title))
    .toEqual(["Typed without a mouse"]);
});

test("the audit notices a control whose only text is hidden from assistive technology", async ({
  page,
}) => {
  await page.goto("/login");
  await expect(page.getByLabel("Username")).toBeVisible();
  expect(await audit(page)).toEqual([]);
  await page.evaluate(() => {
    const button = document.createElement("button");
    button.innerHTML = '<span aria-hidden="true">Delete</span>';
    document.querySelector("main")!.append(button);
    const select = document.createElement("select");
    select.innerHTML = "<option>All lists</option>";
    document.querySelector("main")!.append(select);
  });
  const problems = await audit(page);
  expect(problems.filter((problem) => problem.startsWith("no name"))).toHaveLength(2);
});
