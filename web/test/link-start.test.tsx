import { act, cleanup, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { takeLinkOrigin } from "../src/pages/linked-origin.js";
import { renderApp } from "./support.js";

const left = vi.hoisted(() => vi.fn());
vi.mock("../src/shell/leave.js", () => ({ leave: left }));

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  left.mockClear();
});

test("a link started from outside the dashboard goes to the provider and back to the apps", async () => {
  const app = await renderApp({
    path: "/link/discord",
    hold: true,
    info: {
      login_methods: [
        { key: "discord", type: "discord", name: "Discord", login: true, link: true },
      ],
    },
  });
  await act(() => app.connect());
  const start = "http://hub.test/api/v1/auth/oauth/discord/start?intent=link";
  await vi.waitFor(() => expect(left).toHaveBeenCalledWith(start));
  expect(screen.getByRole("link", { name: "Continue to Discord" }).getAttribute("href")).toBe(
    start,
  );
  expect(takeLinkOrigin()).toBe("/settings/apps");
});
