import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { createServer as createTcpServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import pg from "pg";
import type { TestProject } from "vitest/node";

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

const hubSource = fileURLToPath(new URL("../../../../hub/", import.meta.url));
const proxyHeader = "X-Forwarded-For";

function run(command: string, args: string[], cwd?: string): string {
  try {
    return execFileSync(command, args, {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (cause) {
    throw new Error(
      `The hub tests need Docker and Go: "${command} ${args.slice(0, 2).join(" ")}" failed.`,
      { cause },
    );
  }
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createTcpServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(typeof address === "object" && address ? address.port : 0));
    });
  });
}

async function until(what: string, attempt: () => Promise<boolean>, seconds = 60): Promise<void> {
  const deadline = Date.now() + seconds * 1000;
  while (Date.now() < deadline) {
    if (await attempt().catch(() => false)) return;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Timed out waiting for ${what}.`);
}

export default async function setup(project: TestProject): Promise<() => Promise<void>> {
  const work = mkdtempSync(join(tmpdir(), "plsremind-hub-"));
  const binary = join(work, "plsremind");
  run("go", ["build", "-o", binary, "./cmd/plsremind"], hubSource);

  const container = run("docker", [
    "run",
    "-d",
    "--rm",
    "-e",
    "POSTGRES_PASSWORD=plsremind",
    "-e",
    "POSTGRES_DB=plsremind",
    "-p",
    "127.0.0.1::5432",
    "postgres:18",
  ]).trim();
  const stopDatabase = () => {
    try {
      execFileSync("docker", ["rm", "-f", "-v", container], { stdio: "ignore" });
    } catch {
      // nothing left to stop
    }
  };

  let hub: ChildProcess | undefined;
  const stopHub = async (signal: NodeJS.Signals = "SIGKILL") => {
    const running = hub;
    hub = undefined;
    if (!running || running.exitCode !== null) return;
    const exited = new Promise((resolve) => running.once("exit", resolve));
    running.kill(signal);
    const impatient = setTimeout(() => running.kill("SIGKILL"), 15_000);
    await exited;
    clearTimeout(impatient);
  };

  try {
    const mapped = run("docker", ["port", container, "5432/tcp"]).trim().split("\n")[0]!;
    const databasePort = mapped.slice(mapped.lastIndexOf(":") + 1);
    const databaseUrl = `postgres://postgres:plsremind@127.0.0.1:${databasePort}/plsremind?sslmode=disable`;
    await until("PostgreSQL", async () => {
      const client = new pg.Client({ connectionString: databaseUrl });
      await client.connect();
      await client.end();
      return true;
    });

    const port = await freePort();
    const url = `http://127.0.0.1:${port}`;
    const secret = randomBytes(32).toString("base64");
    const startHub = async () => {
      hub = spawn(binary, [], {
        env: {
          PATH: process.env.PATH ?? "",
          PLSREMIND_DATABASE_URL: databaseUrl,
          PLSREMIND_PUBLIC_URL: url,
          PLSREMIND_LISTEN_ADDR: `127.0.0.1:${port}`,
          PLSREMIND_SECRET_KEY: secret,
          PLSREMIND_PROXY_HEADER: proxyHeader,
        },
        stdio: process.env.PLSREMIND_TEST_LOG ? "inherit" : "ignore",
      });
      await until("the hub", async () => (await fetch(`${url}/api/v1/info`)).ok);
    };
    await startHub();

    const adminPassword = randomBytes(18).toString("base64");
    const created = await fetch(`${url}/api/v1/setup`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username: "admin", password: adminPassword }),
    });
    if (created.status !== 201) throw new Error(`setup answered ${created.status}`);

    const control = createServer((request, response) => {
      const done = (status: number) => response.writeHead(status).end();
      if (request.method !== "POST" || request.url !== "/restart") return done(404);
      stopHub("SIGTERM")
        .then(startHub)
        .then(
          () => done(204),
          () => done(500),
        );
    });
    await new Promise<void>((resolve) => control.listen(0, "127.0.0.1", resolve));
    const controlAddress = control.address();
    const controlPort =
      typeof controlAddress === "object" && controlAddress ? controlAddress.port : 0;

    project.provide("hub", {
      url,
      databaseUrl,
      controlUrl: `http://127.0.0.1:${controlPort}`,
      adminPassword,
    });

    return async () => {
      control.close();
      await stopHub();
      stopDatabase();
      rmSync(work, { recursive: true, force: true });
    };
  } catch (error) {
    await stopHub();
    stopDatabase();
    rmSync(work, { recursive: true, force: true });
    throw error;
  }
}
