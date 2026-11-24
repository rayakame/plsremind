import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const manifest = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const problems = [];
if (manifest.private) problems.push("package.json says private");
const [packed] = JSON.parse(
  execFileSync("npm", ["pack", "--dry-run", "--json"], {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  }),
);
const allowed = /^(package\.json|README\.md|LICENSE|CHANGELOG\.md|dist\/.+)$/;
const paths = packed.files.map((file) => file.path);
for (const path of paths) if (!allowed.test(path)) problems.push(`ships ${path}`);
for (const needed of ["dist/index.js", "dist/index.d.ts", "LICENSE", "README.md"]) {
  if (!paths.includes(needed)) problems.push(`misses ${needed}`);
}
if (problems.length > 0) {
  console.error(problems.join("\n"));
  process.exit(1);
}
console.log(`ok: ${paths.length} files`);
