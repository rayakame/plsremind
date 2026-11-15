import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { renderApp, wireItem, wireUser } from "./support.js";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

async function calendar(now: string, options: Parameters<typeof renderApp>[0] = {}) {
  vi.setSystemTime(new Date(now));
  const app = await renderApp({ path: "/calendar", ...options });
  await screen.findByRole("group", { name: "Days" });
  return app;
}

const cell = (label: string) =>
  within(screen.getByRole("button", { name: label }).closest("[data-day]") as HTMLElement);
const timed = (id: string, title: string, at: string, more = {}) =>
  wireItem({ id, title, due_at: at, ...more });

test("an item sits on its day in the profile's zone, not the browser's", async () => {
  await calendar("2026-11-15T12:00:00Z", {
    user: wireUser({ locale: "en", timezone: "Pacific/Auckland" }),
    sync: {
      items: [timed("a", "Late", "2026-11-15T11:30:00Z"), wireItem({ id: "b", title: "No date" })],
    },
  });
  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("November 2026");
  expect(cell("November 16").getByRole("button", { name: "Late" })).toBeTruthy();
  expect(cell("November 15").queryByRole("button", { name: "Late" })).toBeNull();
  expect(screen.queryByText("No date")).toBeNull();
  expect(screen.getByRole("button", { name: "November 16" }).getAttribute("aria-current")).toBe(
    "date",
  );
});

test("a month with a clock change has every day once and items on the right one", async () => {
  await calendar("2026-03-10T12:00:00Z", {
    sync: {
      items: [
        timed("a", "After midnight", "2026-03-28T23:30:00Z"),
        wireItem({
          id: "b",
          title: "Whole day",
          due_at: "2026-03-29T21:59:59Z",
          due_date: "2026-03-29",
          all_day: true,
        }),
        timed("c", "Evening", "2026-03-29T19:00:00Z"),
      ],
    },
  });
  const days = [...document.querySelectorAll("[data-day]")].map((day) =>
    day.getAttribute("data-day"),
  );
  expect(days).toHaveLength(42);
  expect(new Set(days).size).toBe(42);
  expect(days[0]).toBe("2026-02-23");
  expect(
    cell("March 29")
      .getAllByRole("button")
      .slice(1)
      .map((button) => button.textContent),
  ).toEqual(["Whole day", "After midnight", "Evening"]);
});

test("the week starts where the user says", async () => {
  await calendar("2026-11-15T12:00:00Z", {
    user: wireUser({ locale: "en", preferences: { weekStart: "sunday" } }),
  });
  const heads = [...document.querySelectorAll(".hd")].map((head) => head.textContent);
  expect(heads).toEqual(["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]);
  expect(document.querySelector("[data-day]")!.getAttribute("data-day")).toBe("2026-11-01");
});

test("month and week can be switched and moved through", async () => {
  await calendar("2026-11-15T12:00:00Z");
  await userEvent.click(screen.getByRole("button", { name: "Later" }));
  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("December 2026");
  await userEvent.click(screen.getByRole("button", { name: "Today" }));
  await userEvent.click(screen.getByRole("button", { name: "Week" }));
  expect(document.querySelectorAll("[data-day]")).toHaveLength(7);
  expect(document.querySelector("[data-day]")!.getAttribute("data-day")).toBe("2026-11-09");
  await userEvent.click(screen.getByRole("button", { name: "Earlier" }));
  expect(document.querySelector("[data-day]")!.getAttribute("data-day")).toBe("2026-11-02");
});

test("a chosen day becomes the due date of what is created next", async () => {
  const app = await calendar("2026-11-15T12:00:00Z");
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  await userEvent.click(screen.getByRole("button", { name: "November 20" }));
  expect(screen.getByRole("button", { name: "November 20" }).getAttribute("aria-pressed")).toBe(
    "true",
  );
  await userEvent.type(
    screen.getByRole("combobox", { name: /Search, create/ }),
    "Dentist{Control>}{Enter}{/Control}",
  );
  expect(cell("November 20").getByRole("button", { name: "Dentist" })).toBeTruthy();
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Dentist",
    list_id: "l1",
    due_date: "2026-11-20",
  });
});

test("done items with a date are struck through and a full day folds", async () => {
  await calendar("2026-11-15T12:00:00Z", {
    sync: {
      items: Array.from({ length: 5 }, (_, i) =>
        timed(`i${i}`, `Task ${i}`, `2026-11-18T1${i}:00:00Z`, i === 0 ? { priority: "high" } : {}),
      ),
      history: [
        timed("h", "Was done", "2026-11-17T10:00:00Z", {
          status: "done",
          completed_at: "2026-11-17T09:00:00Z",
        }),
      ],
    },
  });
  expect(cell("November 17").getByRole("button", { name: "Was done" }).className).toContain("done");
  expect(cell("November 18").getByRole("button", { name: "Task 0" }).className).toContain("high");
  expect(cell("November 18").queryByRole("button", { name: "Task 3" })).toBeNull();
  await userEvent.click(cell("November 18").getByRole("button", { name: "2 more" }));
  const day = within(screen.getByRole("dialog"));
  expect(day.getAllByRole("listitem")).toHaveLength(5);
});

test("clicking an item shows its details", async () => {
  await calendar("2026-11-15T12:00:00Z", {
    sync: { items: [timed("a", "Dentist", "2026-11-18T10:00:00Z")] },
  });
  fireEvent.click(screen.getByRole("button", { name: "Dentist" }));
  const panel = within(screen.getByRole("complementary", { name: "Details" }));
  expect((panel.getByLabelText("Title") as HTMLInputElement).value).toBe("Dentist");
});

test("the new command and the bar's text respect the calendar", async () => {
  const app = await calendar("2026-11-15T12:00:00Z", {
    sync: {
      items: [
        timed("a", "Dentist", "2026-11-18T10:00:00Z"),
        timed("b", "Haircut", "2026-11-19T10:00:00Z"),
      ],
    },
  });
  app.route("POST", "/users/@me/items", { status: 201, body: wireItem({ id: "n" }) });
  const bar = screen.getByRole("combobox", { name: /Search, create/ });
  await userEvent.type(bar, "hair");
  expect(screen.queryByRole("button", { name: "Dentist" })).toBeNull();
  expect(screen.getByRole("button", { name: "Haircut" })).toBeTruthy();
  await userEvent.clear(bar);
  await userEvent.click(screen.getByRole("button", { name: "November 20" }));
  await userEvent.type(bar, "/new Vet !high{Enter}");
  await waitFor(() => expect(app.sent("POST", "/users/@me/items")).toHaveLength(1));
  expect(app.sent("POST", "/users/@me/items")[0]!.body).toEqual({
    title: "Vet",
    list_id: "l1",
    priority: "high",
    due_date: "2026-11-20",
  });
});

test("Escape lets go of the chosen day", async () => {
  await calendar("2026-11-15T12:00:00Z");
  await userEvent.click(screen.getByRole("button", { name: "November 20" }));
  await userEvent.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "November 20" }).getAttribute("aria-pressed")).toBe(
    "false",
  );
});

test("switching to the week shows the week of the chosen day", async () => {
  await calendar("2026-11-03T12:00:00Z");
  await userEvent.click(screen.getByRole("button", { name: "November 25" }));
  await userEvent.click(screen.getByRole("button", { name: "Week" }));
  expect(document.querySelector("[data-day]")!.getAttribute("data-day")).toBe("2026-11-23");
  expect(screen.getByRole("button", { name: "November 25" }).getAttribute("aria-pressed")).toBe(
    "true",
  );
});
