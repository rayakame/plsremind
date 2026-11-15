import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer as createTcpServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import pg from "pg";

export interface LaunchedHub {
  url: string;
  databaseUrl: string;
  /** Stops the hub the way a deployment would and starts it again on the same database. */
  restart(): Promise<void>;
  /** The two halves of a restart, for tests that look at the time in between. */
  halt(): Promise<void>;
  resume(): Promise<void>;
  stop(): Promise<void>;
}

const hubSource = fileURLToPath(new URL("../../../../hub/", import.meta.url));

/** Clients behind this header count as separate addresses for the hub's rate limit. */
export const proxyHeader = "X-Forwarded-For";

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

/** Builds the hub and runs it against a throwaway PostgreSQL in Docker. */
export async function launchHub(options: { publicUrl?: string } = {}): Promise<LaunchedHub> {
  const work = mkdtempSync(join(tmpdir(), "plsremind-hub-"));
  const binary = join(work, "plsremind");
  let container = "";
  try {
    run("go", ["build", "-o", binary, "./cmd/plsremind"], hubSource);
    container = run("docker", [
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
  } catch (error) {
    rmSync(work, { recursive: true, force: true });
    throw error;
  }
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
    if (!running || running.exitCode !== null || running.signalCode !== null) return;
    if (running.pid === undefined) return;
    const exited = new Promise((resolve) => running.once("exit", resolve));
    running.kill(signal);
    const impatient = setTimeout(() => running.kill("SIGKILL"), 15_000);
    await exited;
    clearTimeout(impatient);
  };
  const stop = async () => {
    await stopHub();
    stopDatabase();
    rmSync(work, { recursive: true, force: true });
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
          PLSREMIND_PUBLIC_URL: options.publicUrl ?? url,
          PLSREMIND_LISTEN_ADDR: `127.0.0.1:${port}`,
          PLSREMIND_SECRET_KEY: secret,
          PLSREMIND_PROXY_HEADER: proxyHeader,
        },
        stdio: process.env.PLSREMIND_TEST_LOG ? "inherit" : "ignore",
      });
      const child = hub;
      const failed = new Promise<never>((_, reject) => {
        child.once("error", (cause) =>
          reject(new Error("The hub could not be started.", { cause })),
        );
      });
      failed.catch(() => {});
      await Promise.race([
        until("the hub", async () => (await fetch(`${url}/api/v1/info`)).ok),
        failed,
      ]);
    };
    await startHub();
    return {
      url,
      databaseUrl,
      restart: () => stopHub("SIGTERM").then(startHub),
      halt: () => stopHub("SIGTERM"),
      resume: startHub,
      stop,
    };
  } catch (error) {
    await stop();
    throw error;
  }
}
