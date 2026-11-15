// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";

const src = fileURLToPath(new URL("../src", import.meta.url));
const system = join(src, "style", "system.css");

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory() ? files(join(dir, entry.name)) : [join(dir, entry.name)],
  );
}

interface Rule {
  selector: string;
  body: string;
}

function rules(css: string): Rule[] {
  const found: Rule[] = [];
  let at = 0;
  while (at < css.length) {
    const open = css.indexOf("{", at);
    if (open < 0) break;
    let depth = 1;
    let end = open + 1;
    while (depth > 0 && end < css.length) {
      if (css[end] === "{") depth++;
      if (css[end] === "}") depth--;
      end++;
    }
    const selector = css.slice(at, open).trim();
    const body = css.slice(open + 1, end - 1);
    if (selector.startsWith("@media") || selector.startsWith("@supports")) {
      found.push(...rules(body));
    } else {
      found.push({ selector, body });
    }
    at = end;
  }
  return found;
}

const css = readFileSync(system, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const all = rules(css);
const isTokens = (rule: Rule) => rule.selector === ":root" || rule.selector.startsWith(":root[");
const names = (rule: Rule) => [...rule.body.matchAll(/(--[\w-]+)\s*:/g)].map((match) => match[1]!);

test("nothing outside the style file carries a colour or a size", () => {
  const offenders: string[] = [];
  for (const file of files(src)) {
    if (file === system) continue;
    const name = relative(src, file);
    if (file.endsWith(".css")) {
      offenders.push(`${name}: a second style file`);
      continue;
    }
    if (!/\.tsx?$/.test(file)) continue;
    const text = readFileSync(file, "utf8");
    if (/\bstyle=/.test(text)) offenders.push(`${name}: inline style`);
    if (/#[0-9a-fA-F]{3,8}\b/.test(text)) offenders.push(`${name}: colour`);
    if (/\b(rgba?|hsla?)\(/.test(text)) offenders.push(`${name}: colour`);
    if (/\d+px\b/.test(text)) offenders.push(`${name}: size`);
  }
  expect(offenders).toEqual([]);
});

test("the style file keeps raw values in its tokens", () => {
  const offenders: string[] = [];
  for (const rule of all) {
    if (isTokens(rule)) {
      if (/(^|;)\s*[a-z]/.test(rule.body.replace(/--[\w-]+\s*:[^;]*;?/g, ""))) {
        offenders.push(`${rule.selector}: something other than a token`);
      }
      continue;
    }
    if (/#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?)\(/.test(rule.body)) {
      offenders.push(`${rule.selector}: colour`);
    }
    for (const match of rule.body.matchAll(/(\d*\.?\d+)(px|rem|em)\b/g)) {
      if (match[0] !== "1px" && match[0] !== "0px") offenders.push(`${rule.selector}: ${match[0]}`);
    }
  }
  expect(offenders).toEqual([]);
});

test("the spacing scale and the sizes are the agreed ones", () => {
  const root = all.find((rule) => rule.selector === ":root")!;
  const value = (name: string) =>
    new RegExp(`${name}\\s*:\\s*([^;]+);`).exec(root.body)?.[1]?.trim();
  expect(["--s1", "--s2", "--s3", "--s4", "--s5", "--s6", "--s7"].map(value)).toEqual([
    "4px",
    "8px",
    "12px",
    "16px",
    "24px",
    "32px",
    "48px",
  ]);
  expect([value("--h"), value("--r1"), value("--r2")]).toEqual(["44px", "6px", "10px"]);
});

test("dark redefines every colour of light and nothing else", () => {
  const light = all.find((rule) => rule.selector === ":root")!;
  const dark = all.find((rule) => rule.selector === ':root[data-theme="dark"]')!;
  const colours = [...light.body.matchAll(/(--[\w-]+)\s*:\s*(#|rgba?\()/g)].map((m) => m[1]!);
  expect(names(dark).sort()).toEqual(colours.sort());
});
