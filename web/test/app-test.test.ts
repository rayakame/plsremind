import { expect, test } from "vitest";
import { pollDelay } from "../src/pages/settings/app-test.js";

test("a test is looked at often at first and then less", () => {
  expect([0, 1, 4].map(pollDelay)).toEqual([1000, 1000, 1000]);
  expect([5, 20].map(pollDelay)).toEqual([3000, 3000]);
});
