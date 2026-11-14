import { execFileSync, spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { beforeAll, expect, test } from "vitest";
import { hub, personalToken, session, until } from "./helpers.js";

const root = fileURLToPath(new URL("../../", import.meta.url));

beforeAll(() => {
  execFileSync("npm", ["run", "build"], { cwd: root, stdio: "ignore" });
}, 60_000);

test("the example adapter confirms a reminder and exits", async () => {
  const { rest } = await session("mia");
  const token = await personalToken(rest, true);
  const adapter = spawn(process.execPath, ["examples/remind.ts"], {
    cwd: root,
    env: { ...process.env, PLSREMIND_URL: hub.url, PLSREMIND_TOKEN: token, PLSREMIND_ONCE: "1" },
  });
  let output = "";
  adapter.stdout.on("data", (chunk) => (output += chunk));
  adapter.stderr.on("data", (chunk) => (output += chunk));
  const exited = new Promise<number | null>((resolve) => adapter.once("exit", resolve));
  try {
    await until("the adapter to connect", () => output.includes("waiting for reminders"), 20_000);
    const item = await rest.createItem({ title: "example reminder" });
    await rest.createReminder(item.id, { at: new Date(Date.now() + 2000) });
    const code = await Promise.race([
      exited,
      new Promise<string>((resolve) => setTimeout(() => resolve("timeout"), 25_000)),
    ]);
    expect(output).toContain('mia: "example reminder" you asked to be reminded');
    expect(code).toBe(0);
    const log = await rest.listReminderLog({ itemId: item.id });
    expect(log.data[0]!.deliveries[0]!.status).toBe("delivered");
  } finally {
    adapter.kill();
  }
}, 60_000);
