import { expect, test } from "vitest";
import { pollDelay, testShown } from "../src/pages/settings/app-test.js";

test("a test is looked at often at first and then less", () => {
  expect([0, 1, 4].map(pollDelay)).toEqual([1000, 1000, 1000]);
  expect([5, 20].map(pollDelay)).toEqual([3000, 3000]);
});

test("a finished test gives way to a newer delivery, and one under way never does", () => {
  const at = (time: string) => new Date(`2026-11-12T${time}Z`);
  const finished = {
    id: "t",
    connectionId: "a",
    status: "delivered",
    reason: "",
    createdAt: at("18:00:00"),
    closedAt: at("18:00:05"),
  } as const;
  const itself = { status: "delivered", reason: "", at: at("18:00:05"), test: true } as const;
  const newer = {
    status: "failed",
    reason: "DMs closed",
    at: at("19:00:00"),
    test: false,
  } as const;
  expect(testShown(undefined, null)).toBe(false);
  expect(testShown(finished, null)).toBe(true);
  expect(testShown(finished, itself)).toBe(true);
  expect(testShown(finished, newer)).toBe(false);
  expect(testShown({ status: "starting" }, newer)).toBe(true);
  expect(testShown({ status: "lost" }, newer)).toBe(true);
  expect(testShown({ ...finished, status: "pending", closedAt: null }, newer)).toBe(true);
});
