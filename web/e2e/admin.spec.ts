import {
  address,
  admin,
  expect,
  hubUrl,
  restSession,
  signIn,
  test,
  unique,
  watch,
} from "./helpers.js";
import { proxyHeader } from "../../sdk/ts/test/hub/launch.js";

test("an admin invites someone, sees them register, and blocks them", async ({ browser }) => {
  const session = restSession();
  const user = await session.rest.login(admin);
  const adminContext = await browser.newContext({ baseURL: hubUrl });
  await adminContext.setExtraHTTPHeaders({ [proxyHeader]: address() });
  await signIn(adminContext, {
    rest: session.rest,
    user,
    username: admin.username,
    password: admin.password,
    cookie: session.cookie(),
  });
  const page = await adminContext.newPage();
  const problems: string[] = [];
  watch(page, problems);

  await page.goto("/settings/admin/invites");
  await page.getByRole("button", { name: "Create invite" }).click();
  const code = await page.getByRole("dialog").getByLabel("Invite code").inputValue();
  await page.getByRole("dialog").getByRole("button", { name: "Done" }).click();
  await session.rest.updateSettings({ registration: "invite" });

  try {
    const guestContext = await browser.newContext({ baseURL: hubUrl });
    await guestContext.setExtraHTTPHeaders({ [proxyHeader]: address() });
    const guest = await guestContext.newPage();
    const username = unique("guest");
    await guest.goto("/register");
    await guest.getByLabel("Invite code").fill(code);
    await guest.getByLabel("Username").fill(username);
    await guest.getByLabel("Password").fill("a password for the guest");
    await guest.getByRole("button", { name: "Create account" }).click();
    await expect(guest.getByRole("heading", { level: 1, name: "All items" })).toBeVisible();

    await page.goto("/settings/admin/users");
    await expect(page.getByRole("group").first()).toBeVisible();
    const more = page.getByRole("button", { name: "Load more" });
    while (!(await page.getByRole("group", { name: username }).isVisible())) {
      await more.click();
      await expect(more).toBeEnabled();
    }
    await page
      .getByRole("group", { name: username })
      .getByRole("checkbox", { name: "Blocked" })
      .click();
    await expect(guest.getByLabel("Username")).toBeVisible({ timeout: 15_000 });
    await guestContext.close();
  } finally {
    await session.rest.updateSettings({ registration: "open" });
  }
  expect(problems).toEqual([]);
  await adminContext.close();
});
