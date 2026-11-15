import { test as setup } from "@playwright/test";
import { admin, restSession } from "./helpers.js";

setup("the instance gets its admin and opens registration", async () => {
  const { rest } = restSession();
  await rest.setup(admin);
  await rest.updateSettings({ registration: "open" });
});
