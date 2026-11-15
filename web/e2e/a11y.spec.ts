import type { Page } from "@playwright/test";
import { expect, test } from "./helpers.js";

/** Text whose contrast against what lies behind it is below 4.5, and controls without a name. */
async function audit(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const parse = (color: string) => {
      const parts = color.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0, 0];
      return { r: parts[0]!, g: parts[1]!, b: parts[2]!, a: parts[3] ?? 1 };
    };
    const luminance = ({ r, g, b }: { r: number; g: number; b: number }) => {
      const channel = (value: number) => {
        const s = value / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };
    const behind = (element: Element) => {
      let at: Element | null = element;
      const layers: { r: number; g: number; b: number; a: number }[] = [];
      while (at) {
        const color = parse(getComputedStyle(at).backgroundColor);
        if (color.a > 0) layers.push(color);
        if (color.a === 1) break;
        at = at.parentElement;
      }
      let result = { r: 255, g: 255, b: 255 };
      for (const layer of layers.reverse()) {
        result = {
          r: layer.r * layer.a + result.r * (1 - layer.a),
          g: layer.g * layer.a + result.g * (1 - layer.a),
          b: layer.b * layer.a + result.b * (1 - layer.a),
        };
      }
      return result;
    };
    const visible = (element: Element) => {
      const box = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      return box.width > 0 && box.height > 0 && style.visibility !== "hidden";
    };

    const problems: string[] = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const seen = new Set<Element>();
    while (walker.nextNode()) {
      const element = walker.currentNode.parentElement;
      if (!element || seen.has(element) || !walker.currentNode.textContent?.trim()) continue;
      seen.add(element);
      if (!visible(element) || element.closest("[disabled], .sr")) continue;
      const text = parse(getComputedStyle(element).color);
      const back = behind(element);
      const [light, dark] = [luminance(text), luminance(back)].sort((a, b) => b - a);
      const ratio = (light! + 0.05) / (dark! + 0.05);
      if (ratio < 4.5) {
        problems.push(
          `contrast ${ratio.toFixed(2)}: "${element.textContent!.trim().slice(0, 30)}" (${element.className})`,
        );
      }
    }

    for (const control of document.querySelectorAll<HTMLElement>(
      "button, a[href], input, select, textarea",
    )) {
      if (!visible(control)) continue;
      const labelled = control.getAttribute("aria-labelledby");
      const name =
        control.getAttribute("aria-label") ??
        (labelled ? document.getElementById(labelled)?.textContent : null) ??
        (control.id ? document.querySelector(`label[for="${control.id}"]`)?.textContent : null) ??
        control.closest("label")?.textContent ??
        control.textContent;
      if (!name?.trim()) problems.push(`no name: ${control.outerHTML.slice(0, 80)}`);
      if (control.tabIndex < 0) problems.push(`not reachable: ${control.outerHTML.slice(0, 80)}`);
    }
    return problems;
  });
}

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
