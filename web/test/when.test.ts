import { afterEach, expect, test } from "vitest";
import { setLocale } from "../src/i18n/index.js";
import { formatDue } from "../src/time/format.js";
import { parseWhen, splitTrailingWhen } from "../src/time/when.js";

const today = "2026-11-15";

afterEach(() => setLocale("en"));

test("relative days and weekdays are understood", () => {
  expect(parseWhen("today", today)).toEqual({ day: "2026-11-15", time: null });
  expect(parseWhen("Tomorrow 12:00", today)).toEqual({ day: "2026-11-16", time: "12:00" });
  expect(parseWhen("fri", today)).toEqual({ day: "2026-11-20", time: null });
  expect(parseWhen("friday 9:05", today)).toEqual({ day: "2026-11-20", time: "09:05" });
  expect(parseWhen("sunday", today)).toEqual({ day: "2026-11-22", time: null });
  expect(parseWhen("monday", today)).toEqual({ day: "2026-11-16", time: null });
});

test("dates are understood in both notations", () => {
  expect(parseWhen("2026-11-13", today)).toEqual({ day: "2026-11-13", time: null });
  expect(parseWhen("13.11.", today)).toEqual({ day: "2027-11-13", time: null });
  expect(parseWhen("15.11.", today)).toEqual({ day: "2026-11-15", time: null });
  expect(parseWhen("1.12. 8:00", today)).toEqual({ day: "2026-12-01", time: "08:00" });
  expect(parseWhen("13.11.2026 9:05", today)).toEqual({ day: "2026-11-13", time: "09:05" });
});

test("the chosen language is understood next to English", () => {
  expect(parseWhen("heute", today)).toBeNull();
  setLocale("de");
  expect(parseWhen("heute", today)).toEqual({ day: "2026-11-15", time: null });
  expect(parseWhen("morgen 12:00", today)).toEqual({ day: "2026-11-16", time: "12:00" });
  expect(parseWhen("Freitag", today)).toEqual({ day: "2026-11-20", time: null });
  expect(parseWhen("fr.", today)).toEqual({ day: "2026-11-20", time: null });
  expect(parseWhen("do", today)).toBeNull();
  expect(parseWhen("tomorrow", today)).toEqual({ day: "2026-11-16", time: null });
});

test("what is not a day or a time is not guessed", () => {
  for (const text of ["", "soon", "31.02.", "2026-13-01", "today 25:00", "today 12:60", "12:00"]) {
    expect(parseWhen(text, today), text).toBeNull();
  }
  expect(parseWhen("13.11", today)).toBeNull();
});

test("only an expression at the end of a title counts", () => {
  expect(splitTrailingWhen("Call Mia tomorrow 12:00", today)).toEqual({
    rest: "Call Mia",
    when: { day: "2026-11-16", time: "12:00" },
  });
  expect(splitTrailingWhen("Hand in report fri", today)).toEqual({
    rest: "Hand in report",
    when: { day: "2026-11-20", time: null },
  });
  expect(splitTrailingWhen("Read Tomorrow Never Dies", today)).toEqual({
    rest: "Read Tomorrow Never Dies",
    when: null,
  });
  expect(splitTrailingWhen("tomorrow", today)).toEqual({ rest: "tomorrow", when: null });
});

test("a due time reads short and relative to today", () => {
  const item = (dueAt: string | null, dueDate: string | null) =>
    ({ dueAt: dueAt ? new Date(dueAt) : null, dueDate, allDay: dueDate !== null }) as never;
  const zone = "Europe/Berlin";
  expect(formatDue(item(null, null), zone, today)).toBe("");
  expect(formatDue(item("2026-11-15T11:00:00Z", null), zone, today)).toBe("today 12:00 PM");
  expect(formatDue(item("2026-11-15T23:30:00Z", null), zone, today)).toBe("tomorrow 12:30 AM");
  expect(formatDue(item("2026-11-14T22:59:59Z", "2026-11-14"), zone, today)).toBe("yesterday");
  expect(formatDue(item("2026-11-20T22:59:59Z", "2026-11-20"), zone, today)).toBe("Fri");
  expect(formatDue(item("2026-12-24T17:00:00Z", null), zone, today)).toBe("Dec 24 6:00 PM");
  expect(formatDue(item("2027-01-02T22:59:59Z", "2027-01-02"), zone, today)).toBe("Jan 2, 2027");
  setLocale("de");
  expect(formatDue(item("2026-11-15T11:00:00Z", null), zone, today)).toBe("heute 12:00");
  expect(formatDue(item("2026-12-24T17:00:00Z", null), zone, today)).toBe("24. Dez. 18:00");
});
