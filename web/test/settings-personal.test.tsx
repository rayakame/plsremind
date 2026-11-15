import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wirePlan, wireUser } from "./support.js";

afterEach(cleanup);

const me = "/users/@me";

async function personal(options: Parameters<typeof renderApp>[0] = {}) {
  const app = await renderApp({ path: "/settings/personal", ...options });
  app.route("PATCH", me, { status: 200, body: wireUser({ locale: "en" }) });
  app.route("PUT", /reminder-plans/, { status: 200, body: wirePlan() });
  app.route("DELETE", /reminder-plans/, { status: 204 });
  await screen.findByRole("heading", { level: 1, name: "Settings" });
  return app;
}
const sent = (app: Awaited<ReturnType<typeof personal>>) =>
  app.sent("PATCH", me).map((call) => call.body);

test("settings has its areas, and administration only for admins", async () => {
  await personal();
  const areas = within(screen.getByRole("navigation", { name: "Settings" }));
  expect(areas.getAllByRole("link").map((link) => link.textContent)).toEqual([
    "Personal",
    "Connections",
    "Tokens",
    "Login",
  ]);
  expect(areas.getByRole("link", { name: "Personal" }).getAttribute("aria-current")).toBe("page");
});

test("name, zone and language are saved to the profile", async () => {
  const app = await personal();
  const name = screen.getByLabelText("Display name");
  await userEvent.clear(name);
  await userEvent.type(name, "Mia M.");
  await userEvent.tab();
  const zone = screen.getByLabelText("Time zone");
  await userEvent.clear(zone);
  await userEvent.type(zone, "Mars/Olympus");
  await userEvent.tab();
  expect(screen.getByText("This is not a time zone.")).toBeTruthy();
  await userEvent.clear(zone);
  await userEvent.type(zone, "America/New_York");
  await userEvent.tab();
  await userEvent.selectOptions(screen.getByLabelText("Language"), "Deutsch");
  await waitFor(() => expect(sent(app)).toHaveLength(3));
  expect(sent(app)).toEqual([
    { display_name: "Mia M." },
    { timezone: "America/New_York" },
    { locale: "de" },
  ]);
  expect(screen.getByRole("heading", { level: 1, name: "Einstellungen" })).toBeTruthy();
});

test("theme and week start apply at once and keep what other clients stored", async () => {
  const app = await personal({ user: wireUser({ locale: "en", preferences: { other: 1 } }) });
  await userEvent.click(screen.getByRole("button", { name: "Dark" }));
  expect(document.documentElement.dataset.theme).toBe("dark");
  await userEvent.click(screen.getByRole("button", { name: "Sunday" }));
  await waitFor(() => expect(sent(app)).toHaveLength(2));
  expect(sent(app)).toEqual([
    { preferences: { other: 1, theme: "dark" } },
    { preferences: { other: 1, theme: "dark", weekStart: "sunday" } },
  ]);
});

test("quiet hours are switched on with times and off again", async () => {
  const app = await personal();
  await userEvent.click(screen.getByRole("checkbox", { name: "Quiet hours" }));
  await waitFor(() => expect(sent(app)).toHaveLength(1));
  expect(sent(app)[0]).toEqual({ quiet_hours: { start: "22:00", end: "07:00" } });
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "23:30" } });
  await waitFor(() => expect(sent(app)).toHaveLength(2));
  expect(sent(app)[1]).toEqual({ quiet_hours: { start: "23:30", end: "07:00" } });
  await userEvent.click(screen.getByRole("checkbox", { name: "Quiet hours" }));
  await waitFor(() => expect(sent(app)).toHaveLength(3));
  expect(sent(app)[2]).toEqual({ quiet_hours: null });
});

test("a reminder plan takes durations, a follow-up and can go back to the default", async () => {
  const app = await personal({
    sync: {
      plans: [
        wirePlan({ priority: "low" }),
        wirePlan(),
        wirePlan({ priority: "high", before: ["1d", "2h"], follow_up: "daily", is_default: false }),
      ],
    },
  });
  const high = within(screen.getByRole("group", { name: "High priority" }));
  const before = high.getByLabelText("Remind before the deadline");
  expect((before as HTMLInputElement).value).toBe("1d, 2h");
  await userEvent.clear(before);
  await userEvent.type(before, "soon");
  await userEvent.tab();
  expect(high.getByText("Write durations like 1d, 2h.")).toBeTruthy();
  expect(app.sent("PUT", /reminder-plans/)).toHaveLength(0);
  await userEvent.clear(before);
  await userEvent.type(before, "3d, 1h");
  await userEvent.tab();
  await userEvent.selectOptions(high.getByLabelText("After the deadline"), "Twice a day");
  await waitFor(() => expect(app.sent("PUT", /reminder-plans/)).toHaveLength(2));
  expect(app.sent("PUT", /reminder-plans/).map((call) => [call.path, call.body])).toEqual([
    ["/users/@me/reminder-plans/high", { before: ["3d", "1h"], follow_up: "daily" }],
    ["/users/@me/reminder-plans/high", { before: ["3d", "1h"], follow_up: "twice_daily" }],
  ]);
  await userEvent.click(high.getByRole("button", { name: "Back to the default" }));
  await waitFor(() => expect(app.sent("DELETE", "/users/@me/reminder-plans/high")).toHaveLength(1));
  const normal = within(screen.getByRole("group", { name: "Normal priority" }));
  expect(normal.queryByRole("button", { name: "Back to the default" })).toBeNull();
});

test("the command prefix must be one character that is not a letter or digit", async () => {
  const app = await personal();
  const prefix = screen.getByLabelText("Command prefix");
  await userEvent.clear(prefix);
  await userEvent.type(prefix, "a");
  await userEvent.tab();
  expect(
    screen.getByText("Use one character that is not a letter, a digit or a space."),
  ).toBeTruthy();
  expect(sent(app)).toHaveLength(0);
  await userEvent.clear(prefix);
  await userEvent.type(prefix, "!");
  await userEvent.tab();
  await waitFor(() => expect(sent(app)).toHaveLength(1));
  expect(sent(app)[0]).toEqual({ preferences: { prefix: "!" } });
});

test("a key is changed by pressing the new one, refused when taken, and reset", async () => {
  const app = await personal();
  const keys = within(screen.getByRole("group", { name: "Keys" }));
  await userEvent.click(
    keys.getByRole("button", { name: "Change the key for Complete the selected item" }),
  );
  await userEvent.keyboard("j");
  expect(keys.getByRole("alert").textContent).toBe("j is already used for Select the next item.");
  expect(sent(app)).toHaveLength(0);
  await userEvent.keyboard("c");
  await waitFor(() => expect(sent(app)).toHaveLength(1));
  expect(sent(app)[0]).toEqual({ preferences: { keys: { complete: "c" } } });
  expect(keys.queryByRole("alert")).toBeNull();
  await userEvent.click(keys.getByRole("button", { name: "Reset all keys" }));
  await waitFor(() => expect(sent(app)).toHaveLength(2));
  expect(sent(app)[1]).toEqual({ preferences: { keys: {} } });
});
