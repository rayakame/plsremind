import { expect, openNav, test } from "./helpers.js";

test("a list is created, worked through, renamed and deleted @core", async ({ page, account }) => {
  const inbox = (await account.rest.listLists())[0]!;
  await account.rest.createItem({ title: "Water the plants", listId: inbox.id });
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Water the plants" })).toBeVisible();

  await openNav(page);
  await page.getByRole("button", { name: "New list" }).click();
  await page.getByRole("dialog").getByLabel("Name").fill("Errands");
  await page.getByRole("dialog").getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Errands" })).toBeVisible();
  await expect(page).toHaveURL(/\/lists\/[0-9a-f-]{36}$/);

  const listId = page.url().split("/").at(-1)!;
  await account.rest.createItem({ title: "Buy stamps", listId });
  await account.rest.createItem({ title: "Post the letter", listId, priority: "high" });
  await expect(page.getByRole("listitem")).toHaveCount(2);
  await openNav(page);
  await expect(page.getByRole("link", { name: /Errands/ })).toContainText("2");

  await page.keyboard.press("Escape");
  await page.getByRole("checkbox", { name: "Complete Buy stamps" }).click();
  await expect(page.getByRole("listitem")).toHaveCount(1);
  await openNav(page);
  await expect(page.getByRole("link", { name: /Errands/ })).toContainText("1");
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "Rename" }).click();
  await page.getByRole("dialog").getByLabel("Name").fill("Post office");
  await page.getByRole("dialog").getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Post office" })).toBeVisible();

  await page.getByRole("button", { name: "Delete list" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "All items" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Post the letter" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Water the plants" })).toBeVisible();
  await expect
    .poll(async () => (await account.rest.listLists()).map((list) => list.name))
    .toEqual(["Inbox"]);
});

test("text of any length and shape stays inside its row @core", async ({ page, account }) => {
  await account.rest.createItem({ title: "x".repeat(400) });
  await account.rest.createItem({
    title: '<img src=x onerror="document.title=1"> مرحبا '.repeat(8),
  });
  await account.rest.createList({ name: "y".repeat(100) });
  await page.goto("/");
  await expect(page.getByRole("listitem")).toHaveCount(2);
  await expect(page).toHaveTitle("plsremind");
  const overflow = await page.evaluate(() => {
    const root = document.documentElement;
    const rows = [...document.querySelectorAll("[role=listitem]")];
    const beyond = (row: Element) => {
      const edge = row.getBoundingClientRect().right;
      const parts = [...row.querySelectorAll("a, span, input")];
      return Math.max(...parts.map((part) => Math.ceil(part.getBoundingClientRect().right - edge)));
    };
    return {
      page: root.scrollWidth - root.clientWidth,
      rows: rows.map((row) => Math.max(0, beyond(row))),
      clipped: rows.map((row) => getComputedStyle(row.querySelector("a")!).textOverflow),
    };
  });
  expect(overflow).toEqual({ page: 0, rows: [0, 0], clipped: ["ellipsis", "ellipsis"] });
  await openNav(page);
  const nav = await page.locator("nav").boundingBox();
  const link = await page.getByRole("link", { name: /^y+/ }).boundingBox();
  expect(link!.x + link!.width).toBeLessThanOrEqual(nav!.x + nav!.width);
});
