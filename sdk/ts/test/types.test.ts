import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { parse } from "yaml";

const root = new URL("../", import.meta.url);
const spec = parse(readFileSync(new URL("../../docs/api/openapi.yaml", root), "utf8"));
const types = readFileSync(new URL("src/types.ts", root), "utf8");

test("every object the API describes has a type of the same name", () => {
  const schemas = spec.components.schemas as Record<string, { type?: unknown }>;
  const names = Object.keys(schemas).filter((name) => schemas[name]!.type === "object");
  expect(names.length).toBeGreaterThan(20);
  const missing = names
    .filter((name) => name !== "Error")
    .filter((name) => !new RegExp(`export (interface|type) ${name}\\b`).test(types));
  expect(missing).toEqual([]);
});
