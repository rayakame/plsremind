import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { renderApp, wirePlan } from "./support.js";

afterEach(cleanup);

async function reminders() {
  const app = await renderApp({
    path: "/settings/reminders",
    sync: {
      plans: [
        wirePlan({ priority: "low", before: [], follow_up: "never" }),
        wirePlan({ priority: "normal", before: ["1h"], follow_up: "never" }),
        wirePlan({ priority: "high", before: ["1d", "2h"], follow_up: "daily", is_default: false }),
      ],
    },
  });
  app.route("PUT", /reminder-plans/, (call) => ({
    status: 200,
    body: wirePlan({
      ...(call.body as Record<string, unknown>),
      priority: call.path.split("/").at(-1),
    }),
  }));
  app.route("DELETE", /reminder-plans/, { status: 204 });
  await screen.findByRole("heading", { level: 1, name: "When and how often" });
  return app;
}

const plan = (priority: string) =>
  within(screen.getByRole("region", { name: `${priority} priority` }));
const sent = (app: Awaited<ReturnType<typeof reminders>>) =>
  app.sent("PUT", /reminder-plans/).map((call) => [call.path.split("/").at(-1), call.body]);

test("each plan says in words what it does", async () => {
  await reminders();
  expect(
    plan("High").getByText(
      "Reminds you 1 day and 2 hours before the deadline, then daily until it is done.",
    ),
  ).toBeTruthy();
  expect(
    plan("Normal").getByText("Reminds you 1 hour before the deadline and when it comes."),
  ).toBeTruthy();
  expect(plan("Low").getByText("Reminds you only when the deadline comes.")).toBeTruthy();
});

test("the times before the deadline are chips that can be removed and added", async () => {
  const app = await reminders();
  const high = plan("High");
  expect(high.getAllByRole("listitem").map((chip) => chip.textContent)).toEqual([
    "1 day",
    "2 hours",
  ]);
  await userEvent.click(high.getByRole("button", { name: "Remove 2 hours" }));
  await waitFor(() =>
    expect(sent(app)).toEqual([["high", { before: ["1d"], follow_up: "daily" }]]),
  );

  await userEvent.click(plan("Low").getByRole("button", { name: "Add a time" }));
  const field = plan("Low").getByLabelText("New time before the deadline");
  await userEvent.type(field, "soon{Enter}");
  expect(plan("Low").getByText("Write a time like 2h or 1d.")).toBeTruthy();
  await userEvent.clear(field);
  await userEvent.type(field, "3h{Enter}");
  await waitFor(() =>
    expect(sent(app).at(-1)).toEqual(["low", { before: ["3h"], follow_up: "never" }]),
  );
});

test("a time that is already there is not added twice, and there are at most ten", async () => {
  const app = await reminders();
  await userEvent.click(plan("Normal").getByRole("button", { name: "Add a time" }));
  await userEvent.type(plan("Normal").getByLabelText("New time before the deadline"), "60m{Enter}");
  expect(plan("Normal").getByText("This time is already there.")).toBeTruthy();
  expect(sent(app)).toEqual([]);
});

test("after the deadline is one choice, and a changed plan goes back to the default", async () => {
  const app = await reminders();
  await userEvent.click(plan("High").getByRole("button", { name: "Twice a day" }));
  await waitFor(() =>
    expect(sent(app)).toEqual([["high", { before: ["1d", "2h"], follow_up: "twice_daily" }]]),
  );
  await userEvent.click(plan("High").getByRole("button", { name: "Back to the default" }));
  await waitFor(() => expect(app.sent("DELETE", "/users/@me/reminder-plans/high")).toHaveLength(1));
  expect(plan("Normal").queryByRole("button", { name: "Back to the default" })).toBeNull();
});

test("quiet hours say when what falls into them arrives", async () => {
  const app = await renderApp({
    path: "/settings/reminders",
    user: (await import("./support.js")).wireUser({
      locale: "en",
      quiet_hours: { start: "22:00", end: "07:00" },
    }),
  });
  expect(await screen.findByText(/fall into it arrive at 07:00\./)).toBeTruthy();
  expect(app.path()).toBe("/settings/reminders");
});
