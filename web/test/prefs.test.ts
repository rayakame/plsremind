import { expect, test } from "vitest";
import { mergePrefs, readPrefs } from "../src/hub/prefs.js";

const defaults = { theme: "system", weekStart: "monday", prefix: "/", clock: "auto", keys: {} };

test("missing or malformed preferences fall back to the defaults", () => {
  expect(readPrefs({})).toEqual(defaults);
  expect(readPrefs(null)).toEqual(defaults);
  expect(readPrefs("x")).toEqual(defaults);
  expect(readPrefs({ theme: "pink", weekStart: 1, prefix: "ab", keys: [] })).toEqual(defaults);
  expect(readPrefs({ prefix: "a" })).toEqual(defaults);
  expect(readPrefs({ keys: { complete: "c", open: 5, nonsense: "q" } }).keys).toEqual({
    complete: "c",
    nonsense: "q",
  });
});

test("stored preferences are read", () => {
  const stored = {
    theme: "dark",
    weekStart: "sunday",
    prefix: "!",
    clock: "12h",
    keys: { up: "p" },
  };
  expect(readPrefs(stored)).toEqual(stored);
});

test("a change keeps what other clients stored", () => {
  expect(mergePrefs({ other: 1, theme: "dark" }, { prefix: "!" })).toEqual({
    other: 1,
    theme: "dark",
    prefix: "!",
  });
  expect(mergePrefs(null, { theme: "light" })).toEqual({ theme: "light" });
});
