import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterAll, beforeAll, expect, test } from "vitest";

const root = fileURLToPath(new URL("../", import.meta.url));
let out = "";

function imports(file: string): string[] {
  const source = readFileSync(file, "utf8");
  return [...source.matchAll(/(?:from|import)\s*\(?\s*"(\.[^"]+)"/g)].map((match) =>
    resolve(dirname(file), match[1]!),
  );
}

function reachable(entry: string): string[] {
  const seen = new Set<string>();
  const queue = [entry];
  while (queue.length > 0) {
    const file = queue.pop()!;
    if (seen.has(file)) continue;
    seen.add(file);
    queue.push(...imports(file));
  }
  return [...seen].map((file) => relative(out, file)).sort();
}

beforeAll(() => {
  out = mkdtempSync(join(tmpdir(), "plsremind-build-"));
  const tsc = join(root, "../../node_modules/.bin/tsc");
  execFileSync(tsc, ["-p", "tsconfig.build.json", "--outDir", out], { cwd: root });
}, 60_000);

afterAll(() => {
  rmSync(out, { recursive: true, force: true });
});

test("the main entry loads nothing of the optimistic part", () => {
  const files = reachable(join(out, "index.js"));
  expect(files).toContain("client.js");
  expect(files.filter((file) => file.startsWith("optimistic"))).toEqual([]);
});

test("the optimistic part builds on nothing but the public entry", () => {
  const files = imports(join(out, "optimistic/index.js")).map((file) => relative(out, file));
  expect(files.filter((file) => file !== "index.js")).toEqual([]);
});
