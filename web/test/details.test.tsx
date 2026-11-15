import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { change, deferred, renderApp, wireItem, wirePlan, type Answer } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-11T11:00:00Z"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const item = "/users/@me/items/a";

async function opened(options: Parameters<typeof renderApp>[0] = {}) {
  const app = await renderApp(options);
  app.route("PATCH", item, { status: 200, body: wireItem({ id: "a" }) });
  fireEvent.click(screen.getByRole("link", { name: "Hand in the report" }), { detail: 1 });
  const panel = within(screen.getByRole("complementary", { name: "Details" }));
  return { app, panel };
}

test("the title is saved once, when the field is left or Enter is pressed", async () => {
  const { app, panel } = await opened();
  const title = panel.getByLabelText("Title");
  await userEvent.clear(title);
  await userEvent.type(title, "Hand in the thesis");
  expect(app.sent("PATCH", item)).toHaveLength(0);
  await userEvent.tab();
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(1));
  expect(app.sent("PATCH", item)[0]!.body).toEqual({ title: "Hand in the thesis" });
  expect(screen.getByRole("link", { name: "Hand in the thesis" })).toBeTruthy();

  await userEvent.type(panel.getByLabelText("Title"), "!{Enter}");
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(2));
  await userEvent.tab();
  expect(app.sent("PATCH", item)).toHaveLength(2);
});

test("an empty title is not saved", async () => {
  const { app, panel } = await opened();
  await userEvent.clear(panel.getByLabelText("Title"));
  await userEvent.tab();
  expect(app.sent("PATCH", item)).toHaveLength(0);
  expect((panel.getByLabelText("Title") as HTMLInputElement).value).toBe("Hand in the report");
});

test("a change from elsewhere does not overwrite what is being typed", async () => {
  const { app, panel } = await opened();
  const note = panel.getByLabelText("Note");
  await userEvent.type(note, "my draft");
  await app.send("item.updated", change("u1", wireItem({ id: "a", note: "from elsewhere" })));
  expect((note as HTMLTextAreaElement).value).toBe("my draft");
  await userEvent.tab();
  await waitFor(() => expect(app.sent("PATCH", item)[0]!.body).toEqual({ note: "my draft" }));
});

test("a day alone is a whole-day deadline, a time makes it exact, and both can go", async () => {
  const { app, panel } = await opened();
  fireEvent.change(panel.getByLabelText("Due date"), { target: { value: "2026-11-20" } });
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(1));
  expect(app.sent("PATCH", item)[0]!.body).toEqual({ due_date: "2026-11-20" });
  fireEvent.change(panel.getByLabelText("Due time"), { target: { value: "12:00" } });
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(2));
  expect(app.sent("PATCH", item)[1]!.body).toEqual({ due_at: "2026-11-20T11:00:00.000Z" });
  await userEvent.click(panel.getByRole("button", { name: "Remove due date" }));
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(3));
  expect(app.sent("PATCH", item)[2]!.body).toEqual({ due_at: null });
  expect((panel.getByLabelText("Due date") as HTMLInputElement).value).toBe("");
});

test("priority and list change with one call each", async () => {
  const { app, panel } = await opened();
  await userEvent.selectOptions(panel.getByLabelText("Priority"), "High");
  await userEvent.selectOptions(panel.getByLabelText("List"), "Work");
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(2));
  expect(app.sent("PATCH", item).map((call) => call.body)).toEqual([
    { priority: "high" },
    { list_id: "l2" },
  ]);
});

test("subitems are added, ticked, renamed and removed inside the item", async () => {
  const sub = { id: "s1", item_id: "a", title: "Outline", done: false, position: 0 };
  const { app, panel } = await opened({
    sync: {
      items: [wireItem({ id: "a", subitems: [sub] }), wireItem({ id: "b", title: "Other" })],
    },
  });
  app.route("POST", `${item}/subitems`, { status: 201, body: { ...sub, id: "s2" } });
  app.route("PATCH", `${item}/subitems/s1`, { status: 200, body: sub });
  app.route("DELETE", `${item}/subitems/s1`, { status: 204 });

  await userEvent.type(panel.getByLabelText("Add subitem"), "Print it{Enter}");
  expect(panel.getByDisplayValue("Print it")).toBeTruthy();
  expect((panel.getByLabelText("Add subitem") as HTMLInputElement).value).toBe("");
  await waitFor(() => expect(app.sent("POST", `${item}/subitems`)).toHaveLength(1));
  expect(app.sent("POST", `${item}/subitems`)[0]!.body).toEqual({ title: "Print it" });

  await userEvent.click(panel.getByRole("checkbox", { name: "Outline" }));
  await waitFor(() => expect(app.sent("PATCH", `${item}/subitems/s1`)).toHaveLength(1));
  expect(app.sent("PATCH", `${item}/subitems/s1`)[0]!.body).toEqual({ done: true });

  await userEvent.click(panel.getByRole("button", { name: "Remove Outline" }));
  expect(panel.queryByDisplayValue("Outline")).toBeNull();
  await waitFor(() => expect(app.sent("DELETE", `${item}/subitems/s1`)).toHaveLength(1));
});

test("a reminder is set by a duration or a day and time", async () => {
  const { app, panel } = await opened();
  app.route("POST", `${item}/reminders`, {
    status: 201,
    body: {
      id: "r1",
      item_id: "a",
      remind_at: "2026-11-11T13:00:00Z",
      created_at: "2026-11-11T11:00:00Z",
    },
  });
  await userEvent.type(panel.getByLabelText("Remind me"), "2h{Enter}");
  await waitFor(() => expect(app.sent("POST", `${item}/reminders`)).toHaveLength(1));
  expect(app.sent("POST", `${item}/reminders`)[0]!.body).toEqual({
    at: "2026-11-11T13:00:00.000Z",
  });
  await userEvent.type(panel.getByLabelText("Remind me"), "tomorrow 9:00{Enter}");
  await waitFor(() => expect(app.sent("POST", `${item}/reminders`)).toHaveLength(2));
  expect(app.sent("POST", `${item}/reminders`)[1]!.body).toEqual({
    at: "2026-11-12T08:00:00.000Z",
  });
  await userEvent.type(panel.getByLabelText("Remind me"), "whenever{Enter}");
  expect(
    panel.getByText("Write a duration like 2h or a day and time like tomorrow 12:00."),
  ).toBeTruthy();
  expect(app.sent("POST", `${item}/reminders`)).toHaveLength(2);
});

test("the plan can be switched off or replaced for one item", async () => {
  const { app, panel } = await opened();
  await userEvent.selectOptions(panel.getByLabelText("Planned reminders"), "Off");
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(1));
  expect(app.sent("PATCH", item)[0]!.body).toEqual({ reminder_override: { mode: "off" } });
  await userEvent.selectOptions(panel.getByLabelText("Planned reminders"), "Own times");
  const times = panel.getByLabelText("Remind before the deadline");
  await userEvent.type(times, "soon");
  await userEvent.tab();
  expect(panel.getByText("Write durations like 1d, 2h.")).toBeTruthy();
  expect(app.sent("PATCH", item)).toHaveLength(1);
  await userEvent.clear(times);
  await userEvent.type(times, "1d, 2h");
  await userEvent.tab();
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(2));
  expect(app.sent("PATCH", item)[1]!.body).toEqual({
    reminder_override: { mode: "custom", before: ["1d", "2h"] },
  });
  await userEvent.selectOptions(panel.getByLabelText("Planned reminders"), "As planned");
  await waitFor(() => expect(app.sent("PATCH", item)).toHaveLength(3));
  expect(app.sent("PATCH", item)[2]!.body).toEqual({ reminder_override: null });
});

test("upcoming reminders come from the hub and follow a changed plan", async () => {
  const app = await renderApp();
  const first = deferred<Answer>();
  let asked = 0;
  app.route("GET", `${item}/schedule`, () => {
    asked++;
    return asked === 1
      ? first.promise
      : {
          status: 200,
          body: [
            {
              kind: "due",
              offset: null,
              reminder_id: null,
              scheduled_for: "2026-11-13T18:00:00Z",
              fire_at: "2026-11-13T18:00:00Z",
            },
          ],
        };
  });
  fireEvent.click(screen.getByRole("link", { name: "Hand in the report" }), { detail: 1 });
  const panel = within(screen.getByRole("complementary", { name: "Details" }));
  const card = within(panel.getByRole("region", { name: "Upcoming reminders" }));
  expect(card.queryAllByRole("listitem")).toHaveLength(0);
  first.resolve({
    status: 200,
    body: [
      {
        kind: "before",
        offset: "1d",
        reminder_id: null,
        scheduled_for: "2026-11-12T18:00:00Z",
        fire_at: "2026-11-12T18:00:00Z",
      },
      {
        kind: "manual",
        offset: null,
        reminder_id: "r1",
        scheduled_for: "2026-11-12T20:00:00Z",
        fire_at: "2026-11-13T07:00:00Z",
      },
    ],
  });
  await waitFor(() => expect(card.getAllByRole("listitem")).toHaveLength(2));
  expect(card.getAllByRole("listitem").map((row) => row.textContent)).toEqual([
    "1d before" + "tomorrow 19:00",
    "Set by you" + "Fri 08:00",
  ]);
  await app.send("plan.updated", change("u1", wirePlan({ before: ["2h"], is_default: false })));
  await waitFor(() => expect(card.getAllByRole("listitem")).toHaveLength(1));
  expect(card.getByRole("listitem").textContent).toBe("When due" + "Fri 19:00");
});

test("an item deleted elsewhere closes its details", async () => {
  const { app } = await opened();
  await app.send("item.deleted", change("u1", { id: "a" }));
  expect(screen.queryByRole("complementary", { name: "Details" })).toBeNull();
});

test("the details close and open the item large", async () => {
  const { app, panel } = await opened();
  await userEvent.click(panel.getByRole("button", { name: "Open large" }));
  expect(app.path()).toBe("/items/a");
  expect(screen.queryByRole("complementary", { name: "Details" })).toBeNull();
});
