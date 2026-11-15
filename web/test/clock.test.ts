import { afterEach, expect, test } from "vitest";
import { setLocale } from "../src/i18n/index.js";
import { parseClock, showClock, usesHour12 } from "../src/time/clock.js";
import { formatTime } from "../src/time/format.js";

afterEach(() => setLocale("en"));

test("a typed time is read in either notation", () => {
  const cases: Record<string, string | undefined> = {
    "18:00": "18:00",
    "6 pm": "18:00",
    "6:30PM": "18:30",
    "6.30": "06:30",
    "12am": "00:00",
    "12 pm": "12:00",
    "12:05 a.m.": "00:05",
    "7": "07:00",
    "0:00": "00:00",
    "": "",
    "  ": "",
    "24:00": undefined,
    "13pm": undefined,
    "0 am": undefined,
    "6:60": undefined,
    soon: undefined,
  };
  for (const [text, time] of Object.entries(cases)) {
    expect(parseClock(text), JSON.stringify(text)).toBe(time);
  }
});

test("a time is shown on the clock the user chose", () => {
  expect(showClock("18:05", false)).toBe("18:05");
  expect(showClock("18:05", true)).toBe("6:05 PM");
  expect(showClock("00:00", true)).toBe("12:00 AM");
  expect(showClock("12:30", true)).toBe("12:30 PM");
  expect(showClock("", true)).toBe("");
});

test("without a choice the clock follows the language", () => {
  expect(usesHour12("auto")).toBe(true);
  expect(usesHour12("24h")).toBe(false);
  setLocale("de");
  expect(usesHour12("auto")).toBe(false);
  expect(usesHour12("12h")).toBe(true);
});

test("times in lists follow the chosen clock, whatever the language", () => {
  const at = new Date("2026-11-18T17:00:00Z");
  const berlin = (hour12: boolean) => ({ zone: "Europe/Berlin", today: "2026-11-15", hour12 });
  expect(formatTime(at, berlin(false))).toBe("18:00");
  expect(formatTime(at, berlin(true))).toBe("6:00 PM");
  setLocale("de");
  expect(formatTime(at, berlin(true))).toBe("6:00 PM");
  expect(formatTime(at, berlin(false))).toBe("18:00");
});
