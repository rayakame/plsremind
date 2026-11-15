import { expect, test } from "./helpers.js";

test("the hub serves the dashboard without complaints from the browser @core", async ({
  page,
  problems,
}) => {
  const response = await page.goto("/lists/anything");
  expect(response?.status()).toBe(200);
  expect(response?.headers()["content-security-policy"]).toContain("default-src 'self'");
  await expect(page).toHaveTitle("plsremind");
  await expect(page.locator("#app")).not.toBeEmpty();
  expect(problems).toEqual([]);
});
