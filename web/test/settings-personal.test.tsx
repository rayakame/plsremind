import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wirePlan, wireUser } from "./support.js";

afterEach(cleanup);

const me = "/users/@me";

async function personal(options: Parameters<typeof renderApp>[0] = {}) {
  const app = await renderApp({ path: "/settings/personal", ...options });
  app.route("PATCH", me, (call) => ({
    status: 200,
    body: wireUser({ locale: "en", ...(call.body as object) }),
  }));
  app.route("PUT", /reminder-plans/, (call) => ({
    status: 200,
    body: wirePlan(call.body as Record<string, unknown>),
  }));
  app.route("DELETE", /reminder-plans/, { status: 204 });
  await screen.findByRole("navigation", { name: "Settings" });
  return app;
}
const sent = (app: Awaited<ReturnType<typeof personal>>) =>
  app.sent("PATCH", me).map((call) => call.body);

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
  expect(screen.getByRole("navigation", { name: "Einstellungen" })).toBeTruthy();
});

test("theme and week start apply at once and keep what other clients stored", async () => {
  const app = await personal({
    path: "/settings/look",
    user: wireUser({ locale: "en", preferences: { other: 1 } }),
  });
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
  const app = await personal({ path: "/settings/reminders" });
  await userEvent.click(screen.getByRole("checkbox", { name: "Quiet hours" }));
  await waitFor(() => expect(sent(app)).toHaveLength(1));
  expect(sent(app)[0]).toEqual({ quiet_hours: { start: "22:00", end: "07:00" } });
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "23:30" } });
  fireEvent.blur(screen.getByLabelText("From"));
  await waitFor(() => expect(sent(app)).toHaveLength(2));
  expect(sent(app)[1]).toEqual({ quiet_hours: { start: "23:30", end: "07:00" } });
  await userEvent.click(screen.getByRole("checkbox", { name: "Quiet hours" }));
  await waitFor(() => expect(sent(app)).toHaveLength(3));
  expect(sent(app)[2]).toEqual({ quiet_hours: null });
});

test("the command prefix must be one character that is not a letter or digit", async () => {
  const app = await personal({ path: "/settings/keys" });
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
  const app = await personal({ path: "/settings/keys" });
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
