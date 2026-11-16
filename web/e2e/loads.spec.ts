import { expect, test } from "./helpers.js";

test("the hub serves the dashboard without complaints from the browser @core", async ({
  page,
  problems,
}) => {
  const response = await page.goto("/lists/anything");
  expect(response?.status()).toBe(200);
  expect(response?.headers()["content-security-policy"]).toContain("default-src 'self'");
  await expect(page).toHaveTitle("plsremind");
  await expect(page.getByLabel("Username")).toBeVisible();
  expect(problems.filter((problem) => !problem.includes("401"))).toEqual([]);
});
