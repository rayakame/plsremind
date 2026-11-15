import { execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { fileURLToPath } from "node:url";
import { launchHub } from "../../sdk/ts/test/hub/launch.js";

export default async function globalSetup(): Promise<() => Promise<void>> {
  const hubSource = fileURLToPath(new URL("../../hub/", import.meta.url));
  execFileSync("make", ["web"], { cwd: hubSource, stdio: ["ignore", "ignore", "inherit"] });
  const hub = await launchHub();

  const control = createServer((request, response) => {
    const done = (status: number) => response.writeHead(status).end();
    const act = { "/restart": hub.restart, "/halt": hub.halt, "/resume": hub.resume }[
      request.url ?? ""
    ];
    if (request.method !== "POST" || !act) return done(404);
    act().then(
      () => done(204),
      () => done(500),
    );
  });
  await new Promise<void>((resolve) => control.listen(0, "127.0.0.1", resolve));
  const address = control.address();

  process.env.PLSREMIND_E2E_URL = hub.url;
  process.env.PLSREMIND_E2E_DATABASE = hub.databaseUrl;
  process.env.PLSREMIND_E2E_CONTROL = `http://127.0.0.1:${typeof address === "object" && address ? address.port : 0}`;

  return async () => {
    control.close();
    await hub.stop();
  };
}
