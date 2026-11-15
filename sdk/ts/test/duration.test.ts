import { expect, test } from "vitest";
import { formatDuration, parseDuration } from "../src/index.js";

test("reads the hub's duration texts", () => {
  expect(parseDuration("3d")).toBe(259_200_000);
  expect(parseDuration("1d12h")).toBe(129_600_000);
  expect(parseDuration("90m")).toBe(5_400_000);
});

test.each(["", "d", "1h1d", "1h2h", "1 d", "1s", "-1d", "1.5h", "1"])("refuses %j", (text) => {
  expect(() => parseDuration(text)).toThrow(RangeError);
});

test("writes the shortest form the hub reads back", () => {
  expect(formatDuration(129_600_000)).toBe("1d12h");
  expect(formatDuration(5_400_000)).toBe("1h30m");
  for (const ms of [60_000, 86_400_000, 90_060_000]) {
    expect(parseDuration(formatDuration(ms))).toBe(ms);
  }
});

test.each([0, -60_000, 1500, Number.NaN, Number.POSITIVE_INFINITY])("refuses %j", (ms) => {
  expect(() => formatDuration(ms)).toThrow(RangeError);
});
