// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { ApiError, NetworkError } from "plsremind";
import { expect, test } from "vitest";
import { describeError } from "../src/hub/errors.js";
import { en } from "../src/i18n/en.js";

function goFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? goFiles(join(dir, entry.name))
      : entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")
        ? [join(dir, entry.name)]
        : [],
  );
}

test("every error code the hub can answer with has a text", () => {
  const hub = fileURLToPath(new URL("../../hub/internal", import.meta.url));
  const codes = new Set<string>();
  for (const file of goFiles(hub)) {
    const source = readFileSync(file, "utf8");
    for (const match of source.matchAll(/(?:Conflict|NewError)\([^"\n]*"([a-z_]+)"/g)) {
      codes.add(match[1]!);
    }
    for (const match of source.matchAll(/http\.Status\w+:\s+"([a-z_]+)"/g)) codes.add(match[1]!);
  }
  expect(codes.size).toBeGreaterThan(30);
  const missing = [...codes].filter((code) => !(`errors.${code}` in en));
  expect(missing).toEqual([]);
});

test("a failed call is described in the user's words where the code is known", () => {
  expect(describeError(new ApiError(409, "inbox_protected", "The inbox cannot be removed."))).toBe(
    "The inbox cannot be deleted.",
  );
  expect(describeError(new ApiError(409, "brand_new_code", "Said by the hub."))).toBe(
    "Said by the hub.",
  );
  expect(
    describeError(
      new ApiError(422, "validation_failed", "The request is invalid.", { title: "is too long" }),
    ),
  ).toBe("Please check your input. is too long");
  expect(describeError(new NetworkError("down"))).toBe(
    "The hub did not answer. Check your connection.",
  );
  expect(describeError(new Error("x"))).toBe("Something went wrong.");
});
