import { expect, test as setup } from "@playwright/test";
import { address, admin, restSession } from "./helpers.js";
import { proxyHeader } from "../../sdk/ts/test/hub/launch.js";

setup("a fresh instance is set up in the browser", async ({ page, context }) => {
  await context.setExtraHTTPHeaders({ [proxyHeader]: address() });
  await page.goto("/lists/somewhere");
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole("heading", { name: "Set up plsremind" })).toBeVisible();
  await page.getByLabel("Username").fill(admin.username);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("link", { name: /Inbox/ })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);

  const { rest } = restSession();
  await rest.login(admin);
  await rest.updateSettings({ registration: "open" });
});
