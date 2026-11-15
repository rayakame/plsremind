import { randomBytes } from "node:crypto";
import { createServer } from "node:http";
import type { TestProject } from "vitest/node";
import { launchHub } from "./launch.js";

export interface HubUnderTest {
  url: string;
  databaseUrl: string;
  controlUrl: string;
  adminPassword: string;
}

declare module "vitest" {
  export interface ProvidedContext {
    hub: HubUnderTest;
  }
}

export default async function setup(project: TestProject): Promise<() => Promise<void>> {
  const hub = await launchHub();
  try {
    const adminPassword = randomBytes(18).toString("base64");
    const created = await fetch(`${hub.url}/api/v1/setup`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username: "admin", password: adminPassword }),
    });
    if (created.status !== 201) throw new Error(`setup answered ${created.status}`);

    const control = createServer((request, response) => {
      const done = (status: number) => response.writeHead(status).end();
      if (request.method !== "POST" || request.url !== "/restart") return done(404);
      hub.restart().then(
        () => done(204),
        () => done(500),
      );
    });
    await new Promise<void>((resolve) => control.listen(0, "127.0.0.1", resolve));
    const controlAddress = control.address();
    const controlPort =
      typeof controlAddress === "object" && controlAddress ? controlAddress.port : 0;

    project.provide("hub", {
      url: hub.url,
      databaseUrl: hub.databaseUrl,
      controlUrl: `http://127.0.0.1:${controlPort}`,
      adminPassword,
    });

    return async () => {
      control.close();
      await hub.stop();
    };
  } catch (error) {
    await hub.stop();
    throw error;
  }
}
