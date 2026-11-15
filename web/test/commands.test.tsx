import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { renderApp, wireItem, wireUser } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-11T11:00:00Z"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const bar = () => screen.getByRole("combobox", { name: /Search, create/ });
const preview = () => screen.getByRole("status", { name: "What was understood" }).textContent;
const select = (title: string) =>
  fireEvent.click(screen.getByRole("link", { name: title }), { detail: 1 });

async function app(options: Parameters<typeof renderApp>[0] = {}) {
  const made = await renderApp(options);
  made.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  made.route("PATCH", /^\/users\/@me\/items\/[ab]$/, { status: 200, body: wireItem({ id: "a" }) });
  made.route("POST", /reminders$/, {
    status: 201,
    body: {
      id: "r1",
      item_id: "a",
      remind_at: "2026-11-11T13:00:00Z",
      created_at: "2026-11-11T11:00:00Z",
    },
  });
  made.route("PATCH", "/users/@me", { status: 200, body: wireUser({ locale: "en" }) });
  return made;
}

test("the prefix shows the commands, and typing narrows them", async () => {
  await app();
  await userEvent.type(bar(), "/");
  const options = () => within(screen.getByRole("listbox")).getAllByRole("option");
  expect(options().map((option) => option.textContent)).toEqual([
    "/new title [when] [!priority] [#list]" + "Create an item",
    "/find text" + "Search open and done items",
    "/go place" + "Go to a list, calendar, done or settings",
    "/done" + "Complete the selected item",
    "/remind duration" + "Remind about the selected item",
    "/due when" + "Set when the selected item is due",
    "/move #list" + "Move the selected item",
    "/prio low|normal|high" + "Set the priority of the selected item",
    "/theme system|light|dark" + "Change the colour scheme",
  ]);
  expect(bar().getAttribute("aria-expanded")).toBe("true");
  await userEvent.type(bar(), "d");
  expect(options()).toHaveLength(2);
  await userEvent.keyboard("{Tab}");
  expect((bar() as HTMLInputElement).value).toBe("/done ");
});

test("a new item is previewed and created with what was understood", async () => {
  const made = await app();
  await userEvent.type(bar(), "/new Call Mia tomorrow 12:00 !high #wor");
  expect(preview()).toBe("Call Mia" + "tomorrow 12:00 PM" + "High" + "Work");
  await userEvent.keyboard("{Enter}");
  expect((bar() as HTMLInputElement).value).toBe("");
  await waitFor(() => expect(made.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(made.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Call Mia",
    list_id: "l2",
    priority: "high",
    due_at: "2026-11-12T11:00:00.000Z",
  });
});

test("a list nobody has is said in the preview and stays in the title", async () => {
  await app();
  await userEvent.type(bar(), "/new Buy #1 seller");
  expect(preview()).toBe("Buy #1 seller" + "No list starts with 1");
});

test("a command about an item needs one selected", async () => {
  const made = await app();
  await userEvent.type(bar(), "/done");
  expect(preview()).toBe("Select an item first.");
  await userEvent.keyboard("{Enter}");
  expect(made.sent("PATCH", /items/)).toHaveLength(0);
  expect((bar() as HTMLInputElement).value).toBe("/done");
});

test("the selected item is completed, moved, dated, reminded and prioritised", async () => {
  const made = await app();
  const run = async (text: string) => {
    await userEvent.clear(bar());
    await userEvent.type(bar(), `${text}{Enter}`);
  };
  select("Hand in the report");
  await run("/prio high");
  await run("/move #work");
  await run("/due fri 9:00");
  await run("/due -");
  await run("/remind 2h");
  await run("/done");
  await waitFor(() => expect(made.sent("PATCH", "/users/@me/items/a")).toHaveLength(5));
  expect(made.sent("PATCH", "/users/@me/items/a").map((call) => call.body)).toEqual([
    { priority: "high" },
    { list_id: "l2" },
    { due_at: "2026-11-13T08:00:00.000Z" },
    { due_at: null },
    { status: "done" },
  ]);
  expect(made.sent("POST", /reminders$/)[0]!.body).toEqual({ at: "2026-11-11T13:00:00.000Z" });
});

test("a command that makes no sense is not run and says how it is meant", async () => {
  const made = await app();
  select("Hand in the report");
  await userEvent.type(bar(), "/prio urgent");
  expect(preview()).toBe("/prio low|normal|high");
  await userEvent.keyboard("{Enter}");
  expect(made.sent("PATCH", /items/)).toHaveLength(0);
  await userEvent.clear(bar());
  await userEvent.type(bar(), "/nonsense");
  expect(preview()).toBe("No such command.");
});

test("go leads to lists and pages", async () => {
  const made = await app();
  await userEvent.type(bar(), "/go wor");
  expect(preview()).toBe("Work");
  await userEvent.keyboard("{Enter}");
  expect(made.path()).toBe("/lists/l2");
  await userEvent.type(bar(), "/go calendar{Enter}");
  expect(made.path()).toBe("/calendar");
  await userEvent.type(bar(), "/go done{Enter}");
  expect(made.path()).toBe("/done");
  await userEvent.type(bar(), "/go settings{Enter}");
  expect(made.path()).toBe("/settings/personal");
});

test("find lists open and done items that match", async () => {
  const made = await app({
    sync: {
      items: [
        wireItem({ id: "a", title: "Hand in the report" }),
        wireItem({ id: "b", title: "Call Mia" }),
      ],
      history: [
        wireItem({
          id: "h",
          title: "Old report",
          status: "done",
          completed_at: "2026-11-10T10:00:00Z",
        }),
      ],
    },
  });
  await userEvent.type(bar(), "/find report{Enter}");
  expect(made.path()).toBe("/find?q=report");
  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Results for report");
  expect(
    screen.getAllByRole("listitem").map((row) => within(row).getByRole("link").textContent),
  ).toEqual(["Hand in the report", "Old report"]);
});

test("the theme changes at once and is stored with the other preferences", async () => {
  const made = await app({ user: wireUser({ locale: "en", preferences: { other: 1 } }) });
  await userEvent.type(bar(), "/theme dark{Enter}");
  expect(document.documentElement.dataset.theme).toBe("dark");
  await waitFor(() => expect(made.sent("PATCH", "/users/@me")).toHaveLength(1));
  expect(made.sent("PATCH", "/users/@me")[0]!.body).toEqual({
    preferences: { other: 1, theme: "dark" },
  });
});

test("another prefix replaces the slash", async () => {
  const made = await app({ user: wireUser({ locale: "en", preferences: { prefix: "!" } }) });
  await userEvent.type(bar(), "/go calendar");
  expect(screen.queryByRole("listbox")).toBeNull();
  await userEvent.clear(bar());
  await userEvent.type(bar(), "!go calendar{Enter}");
  expect(made.path()).toBe("/calendar");
});

test("after a command the bar lets go, so that single keys work again", async () => {
  const made = await app();
  await userEvent.type(bar(), "/go all{Enter}");
  expect(document.activeElement).not.toBe(bar());
  await userEvent.keyboard("g");
  expect((bar() as HTMLInputElement).value).toBe("/go ");
  await userEvent.keyboard("calendar{Enter}");
  expect(made.path()).toBe("/calendar");
  expect(document.activeElement).not.toBe(bar());
});

const choices = () =>
  within(screen.getByRole("listbox"))
    .getAllByRole("option")
    .map((option) => option.textContent);

test("a command shows what its argument may be, and typing narrows it", async () => {
  const made = await app();
  await userEvent.type(bar(), "/go ");
  expect(choices()).toEqual([
    "all" + "All items",
    "calendar" + "Calendar",
    "done" + "Done",
    "settings" + "Settings",
    "Inbox" + "List",
    "Work" + "List",
  ]);
  await userEvent.type(bar(), "c");
  expect(choices()).toEqual(["calendar" + "Calendar"]);
  await userEvent.keyboard("{ArrowDown}{Enter}");
  expect(made.path()).toBe("/calendar");
});

test("Tab fills in the first choice and a click runs it", async () => {
  const made = await app();
  select("Hand in the report");
  await userEvent.type(bar(), "/prio ");
  expect(choices()).toEqual(["low" + "Low", "normal" + "Normal", "high" + "High"]);
  await userEvent.type(bar(), "h{Tab}");
  expect((bar() as HTMLInputElement).value).toBe("/prio high");
  await userEvent.clear(bar());
  await userEvent.type(bar(), "/move ");
  expect(choices()).toEqual(["#Inbox", "#Work"]);
  await userEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: "#Work" }));
  await waitFor(() => expect(made.sent("PATCH", "/users/@me/items/a")).toHaveLength(1));
  expect(made.sent("PATCH", "/users/@me/items/a")[0]!.body).toEqual({ list_id: "l2" });
  expect((bar() as HTMLInputElement).value).toBe("");
});

test("the schemes and the examples for times are shown", async () => {
  await app();
  select("Hand in the report");
  await userEvent.type(bar(), "/theme ");
  expect(choices()).toEqual(["system" + "System", "light" + "Light", "dark" + "Dark"]);
  await userEvent.clear(bar());
  await userEvent.type(bar(), "/due ");
  expect(preview()).toBe("today, tomorrow 12:00, fri, 13.11., 2026-11-13, or - for none");
  await userEvent.clear(bar());
  await userEvent.type(bar(), "/remind ");
  expect(preview()).toBe("30m, 2h, 1d");
});

test("inside a new item the lists follow a hash and the priorities an exclamation mark", async () => {
  await app();
  await userEvent.type(bar(), "/new Call Mia #");
  expect(choices()).toEqual(["#Inbox", "#Work"]);
  await userEvent.type(bar(), "w{Tab}");
  expect((bar() as HTMLInputElement).value).toBe("/new Call Mia #work ");
  await userEvent.type(bar(), "!");
  expect(choices()).toEqual(["!low" + "Low", "!normal" + "Normal", "!high" + "High"]);
  await userEvent.keyboard("{ArrowDown}{ArrowDown}{ArrowDown}{Enter}");
  expect((bar() as HTMLInputElement).value).toBe("/new Call Mia #work !high ");
  expect(preview()).toBe("Call Mia" + "High" + "Work");
});
