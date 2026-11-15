import { expect, test } from "vitest";
import { localPath } from "../src/shell/router.js";

test("only a path inside the dashboard is followed after signing in", () => {
  expect(localPath("/lists/l2?x=1")).toBe("/lists/l2?x=1");
  for (const bad of [
    null,
    "",
    "lists",
    "//evil.example",
    "/\\evil.example",
    "/\t/evil.example",
    "/\n/x",
    "https://evil.example",
  ]) {
    expect(localPath(bad), String(bad)).toBe("/");
  }
});
