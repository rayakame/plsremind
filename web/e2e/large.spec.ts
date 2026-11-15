import { expect, openNav, seedOpen, test } from "./helpers.js";

test("five thousand items stay quick to scroll, filter and complete", async ({ page, account }) => {
  await seedOpen(account.user.id, 5000);
  await page.goto("/");
  await openNav(page);
  await expect(page.getByRole("link", { name: /All items/ })).toContainText("5000");
  expect(await page.getByRole("listitem").count()).toBeLessThan(100);

  const filtered = await page.evaluate(async () => {
    const input = document.querySelector<HTMLInputElement>('[role="combobox"]')!;
    const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    const start = performance.now();
    set.call(input, "Seeded 49");
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
    const titles = [...document.querySelectorAll('[role="listitem"] a')].map((a) => a.textContent);
    return { ms: performance.now() - start, titles };
  });
  expect(filtered.titles.length).toBeGreaterThan(0);
  expect(filtered.titles.length).toBeLessThan(100);
  expect(filtered.titles.every((title) => title!.includes("49"))).toBe(true);
  expect(filtered.ms).toBeLessThan(150);

  await page.getByRole("combobox", { name: /Search, create/ }).press("Escape");
  const completed = await page.evaluate(async () => {
    const row = document.querySelector('[role="listitem"]')!;
    const title = row.querySelector("a")!.textContent;
    const start = performance.now();
    row.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click();
    await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
    const titles = [...document.querySelectorAll('[role="listitem"] a')].map((a) => a.textContent);
    return { ms: performance.now() - start, gone: !titles.includes(title) };
  });
  expect(completed.gone).toBe(true);
  expect(completed.ms).toBeLessThan(150);

  for (let i = 0; i < 50; i++) await page.keyboard.press("j");
  await expect(page.locator('[role="listitem"][aria-current="true"]')).toBeInViewport();
  expect(await page.getByRole("listitem").count()).toBeLessThan(100);
});
