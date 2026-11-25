import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { renderApp, wireUser } from "./support.js";

afterEach(cleanup);

const admin = wireUser({ locale: "en", role: "admin" });

async function instance(version: string) {
  const app = await renderApp({ path: "/settings/admin/instance", user: admin, info: { version } });
  app.route("GET", "/admin/settings", {
    status: 200,
    body: { name: "plsremind", registration: "invite" },
  });
  return within(await screen.findByRole("region", { name: "Version" }));
}

test("a released version links to its release notes", async () => {
  const card = await instance("0.3.1");
  expect(card.getByText("plsremind 0.3.1")).toBeTruthy();
  expect(card.getByRole("link", { name: "Release notes" }).getAttribute("href")).toBe(
    "https://github.com/rayakame/plsremind/releases/tag/v0.3.1",
  );
});

test("a build that is not a release shows its version without a link", async () => {
  const card = await instance("dev");
  expect(card.getByText("plsremind dev")).toBeTruthy();
  expect(card.queryByRole("link")).toBeNull();
});
