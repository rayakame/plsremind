// Fails when the scripts the start page needs outgrow their budget.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const budget = 200 * 1024;
const dist = fileURLToPath(new URL("../dist/", import.meta.url));
const manifest = JSON.parse(readFileSync(`${dist}.vite/manifest.json`, "utf8"));

const entry = Object.values(manifest).find((chunk) => chunk.isEntry);
if (!entry) throw new Error("the build has no entry");

const files = new Set();
const visit = (chunk) => {
  if (files.has(chunk.file)) return;
  files.add(chunk.file);
  for (const name of chunk.imports ?? []) visit(manifest[name]);
};
visit(entry);

let total = 0;
for (const file of files) {
  const size = gzipSync(readFileSync(dist + file)).length;
  total += size;
  console.log(`${(size / 1024).toFixed(1).padStart(7)} kB  ${file}`);
}
console.log(`${(total / 1024).toFixed(1).padStart(7)} kB  in all, of ${budget / 1024} kB`);
if (total > budget) {
  console.error("The start page's scripts are over budget. Move code behind a lazy route.");
  process.exit(1);
}
