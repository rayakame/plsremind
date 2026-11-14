import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { hubEventNames } from "../src/events.js";

test("every event the gateway documents has a type", () => {
  const doc = readFileSync(new URL("../../../docs/gateway.md", import.meta.url), "utf8");
  const table = doc.slice(doc.indexOf("## Events at a glance"), doc.indexOf("## Example"));
  const documented = [...table.matchAll(/`([a-z]+\.[a-z_]+)`/g)].map((match) => match[1]!);
  expect(documented.length).toBeGreaterThan(15);
  expect([...new Set(documented)].sort()).toEqual([...hubEventNames].sort());
});
