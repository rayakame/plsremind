import { expect, test } from "vitest";
import {
  addDays,
  dayIn,
  instantAt,
  monthGrid,
  timeIn,
  weekStartOf,
  weekdayOf,
} from "../src/time/zone.js";

const berlin = "Europe/Berlin";

test("a day is the calendar day in the given zone", () => {
  expect(dayIn(new Date("2026-11-15T23:30:00Z"), berlin)).toBe("2026-11-16");
  expect(dayIn(new Date("2026-11-15T23:30:00Z"), "America/Los_Angeles")).toBe("2026-11-15");
  expect(dayIn(new Date("2026-11-15T11:30:00Z"), "Pacific/Auckland")).toBe("2026-11-16");
  expect(timeIn(new Date("2026-11-15T23:30:00Z"), berlin)).toBe("00:30");
  expect(timeIn(new Date("2026-11-15T23:05:00Z"), "UTC")).toBe("23:05");
});

test("a local time becomes the right instant on both sides of a clock change", () => {
  expect(instantAt("2026-03-28", "12:00", berlin).toISOString()).toBe("2026-03-28T11:00:00.000Z");
  expect(instantAt("2026-03-29", "12:00", berlin).toISOString()).toBe("2026-03-29T10:00:00.000Z");
  expect(instantAt("2026-10-25", "12:00", berlin).toISOString()).toBe("2026-10-25T11:00:00.000Z");
  expect(instantAt("2026-03-29", "00:00", berlin).toISOString()).toBe("2026-03-28T23:00:00.000Z");
  expect(instantAt("2026-11-15", "00:00", "Pacific/Auckland").toISOString()).toBe(
    "2026-11-14T11:00:00.000Z",
  );
});

test("a time the clocks skip moves forward by the gap", () => {
  const instant = instantAt("2026-03-29", "02:30", berlin);
  expect(timeIn(instant, berlin)).toBe("03:30");
  expect(dayIn(instant, berlin)).toBe("2026-03-29");
});

test("days are counted on the calendar, not in hours", () => {
  expect(addDays("2026-10-25", 1)).toBe("2026-10-26");
  expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
  expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
  expect(weekdayOf("2026-11-15")).toBe(6);
  expect(weekdayOf("2026-11-16")).toBe(0);
});

test("a week starts where the user says", () => {
  expect(weekStartOf("2026-11-15", "monday")).toBe("2026-11-09");
  expect(weekStartOf("2026-11-15", "sunday")).toBe("2026-11-15");
  expect(weekStartOf("2026-11-16", "sunday")).toBe("2026-11-15");
  expect(weekStartOf("2026-11-16", "monday")).toBe("2026-11-16");
});

test("a month is shown as six whole weeks", () => {
  const grid = monthGrid("2026-11", "monday");
  expect(grid).toHaveLength(42);
  expect(grid[0]).toBe("2026-10-26");
  expect(grid[41]).toBe("2026-12-06");
  expect(monthGrid("2026-11", "sunday")[0]).toBe("2026-11-01");
  expect(new Set(monthGrid("2026-03", "monday")).size).toBe(42);
});
