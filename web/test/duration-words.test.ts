import { afterEach, expect, test } from "vitest";
import { setLocale } from "../src/i18n/index.js";
import { durationWords, listWords } from "../src/time/duration-words.js";

afterEach(() => setLocale("en"));

test("durations are said in words", () => {
  setLocale("en");
  expect(durationWords(86_400_000)).toBe("1 day");
  expect(durationWords(2 * 86_400_000)).toBe("2 days");
  expect(durationWords(90 * 60_000)).toBe("1 hour and 30 minutes");
  expect(durationWords(86_400_000 + 2 * 3_600_000 + 60_000)).toBe("1 day, 2 hours and 1 minute");
  setLocale("de");
  expect(durationWords(86_400_000 + 2 * 3_600_000)).toBe("1 Tag und 2 Stunden");
  expect(durationWords(60_000)).toBe("1 Minute");
});

test("lists are joined the way people say them", () => {
  setLocale("en");
  expect(listWords([])).toBe("");
  expect(listWords(["a"])).toBe("a");
  expect(listWords(["a", "b"])).toBe("a and b");
  expect(listWords(["a", "b", "c"])).toBe("a, b and c");
});
