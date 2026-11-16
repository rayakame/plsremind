import { defineConfig, devices } from "@playwright/test";

const phone = { viewport: { width: 360, height: 740 }, hasTouch: true, isMobile: true };
const main = ["desktop-light", "desktop-dark", "phone-light", "phone-dark"];
// These change the instance for everyone, so they run alone once the others are through.
const alone = ["admin.spec.ts", "restart.spec.ts"];

export default defineConfig({
  testDir: "e2e",
  globalSetup: "./e2e/global-setup.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  timeout: 30_000,
  use: {
    ...devices["Desktop Chrome"],
    baseURL: process.env.PLSREMIND_E2E_URL,
    trace: "retain-on-failure",
  },
  projects: [
    { name: "first", testMatch: "first.setup.ts" },
    {
      name: "desktop-light",
      dependencies: ["first"],
      testIgnore: alone,
      use: { colorScheme: "light" },
    },
    {
      name: "desktop-dark",
      dependencies: ["first"],
      grep: /@core/,
      testIgnore: alone,
      use: { colorScheme: "dark" },
    },
    {
      name: "phone-light",
      dependencies: ["first"],
      grep: /@core/,
      testIgnore: alone,
      use: { ...phone, colorScheme: "light" },
    },
    {
      name: "phone-dark",
      dependencies: ["first"],
      grep: /@core/,
      testIgnore: alone,
      use: { ...phone, colorScheme: "dark" },
    },
    { name: "admin", dependencies: main, testMatch: "admin.spec.ts" },
    { name: "restart", dependencies: ["admin"], testMatch: "restart.spec.ts" },
  ],
});
