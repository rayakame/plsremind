import type { List } from "plsremind";
import { afterEach, expect, test } from "vitest";
import { complete, parseCommand, parseNew } from "../src/bar/parse.js";
import { setLocale } from "../src/i18n/index.js";

const list = (id: string, name: string): List => ({
  id,
  name,
  position: 0,
  isInbox: id === "l1",
  createdAt: new Date(0),
  updatedAt: new Date(0),
});
const lists = [list("l1", "Inbox"), list("l2", "Work"), list("l3", "Workshop"), list("l4", "Home")];
const ctx = { today: "2026-11-15", lists };

afterEach(() => setLocale("en"));

test("a new item's time, priority and list are read off its title", () => {
  expect(parseNew("Call Mia tomorrow 12:00 !high #ho", ctx)).toEqual({
    title: "Call Mia",
    when: { day: "2026-11-16", time: "12:00" },
    priority: "high",
    list: lists[3],
    unknownList: null,
  });
  expect(parseNew("#home !low Water the plants fri", ctx)).toMatchObject({
    title: "Water the plants",
    when: { day: "2026-11-20", time: null },
    priority: "low",
    list: lists[3],
  });
  expect(parseNew("Just a title", ctx)).toEqual({
    title: "Just a title",
    when: null,
    priority: null,
    list: null,
    unknownList: null,
  });
});

test("a list is found by the start of its name when that is unambiguous", () => {
  expect(parseNew("x #work", ctx).list).toBe(lists[1]);
  expect(parseNew("x #works", ctx).list).toBe(lists[2]);
  expect(parseNew("x #wor", ctx)).toMatchObject({
    title: "x #wor",
    list: null,
    unknownList: "wor",
  });
  expect(parseNew("Buy #1 seller", ctx)).toMatchObject({
    title: "Buy #1 seller",
    list: null,
    unknownList: "1",
  });
});

test("what is not recognised stays in the title", () => {
  expect(parseNew("Fix it !urgent", ctx)).toMatchObject({
    title: "Fix it !urgent",
    priority: null,
  });
  expect(parseNew("Mail a#b@example.com wow!", ctx)).toMatchObject({
    title: "Mail a#b@example.com wow!",
    list: null,
    unknownList: null,
  });
  expect(parseNew("Read Tomorrow Never Dies", ctx)).toMatchObject({
    title: "Read Tomorrow Never Dies",
    when: null,
  });
  expect(parseNew("tomorrow", ctx)).toMatchObject({ title: "tomorrow", when: null });
  expect(parseNew("   ", ctx).title).toBe("");
});

test("priorities are understood in English and the chosen language", () => {
  setLocale("de");
  expect(parseNew("Steuer morgen !hoch", ctx)).toMatchObject({
    title: "Steuer",
    when: { day: "2026-11-16", time: null },
    priority: "high",
  });
  expect(parseNew("Tax !high", ctx).priority).toBe("high");
});

test("a command is the prefix, a name and the rest", () => {
  expect(parseCommand("/new Call Mia", "/")).toEqual({ name: "new", rest: "Call Mia" });
  expect(parseCommand("/DONE", "/")).toEqual({ name: "done", rest: "" });
  expect(parseCommand("/", "/")).toEqual({ name: "", rest: "" });
  expect(parseCommand("new", "/")).toBeNull();
  expect(parseCommand("!go work", "!")).toEqual({ name: "go", rest: "work" });
  expect(parseCommand("/go work", "!")).toBeNull();
});

test("Tab finishes the name of a command", () => {
  const names = ["new", "find", "go", "done", "due"];
  expect(complete("/pr", "/", ["prio"])).toBe("/prio ");
  expect(complete("/d", "/", names)).toBe("/done ");
  expect(complete("/x", "/", names)).toBeNull();
  expect(complete("/go wo", "/", names)).toBeNull();
});
