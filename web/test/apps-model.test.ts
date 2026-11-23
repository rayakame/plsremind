import type { LoginMethod, UserConnection } from "plsremind";
import { expect, test } from "vitest";
import {
  accessOf,
  grantFor,
  levelsOf,
  needsAttention,
  sectionsOf,
  statusOf,
} from "../src/pages/settings/apps-model.js";

function app(overrides: Partial<UserConnection> = {}): UserConnection {
  return {
    id: "c1",
    name: "plsremind",
    description: "",
    provider: "discord",
    linked: true,
    enabled: true,
    remind: true,
    intents: ["deliveries"],
    installUrl: "",
    lastDelivery: null,
    ...overrides,
  };
}

const at = new Date("2026-11-12T18:00:00Z");

test("only a failed last reminder of an app that reminds needs attention", () => {
  expect(needsAttention([])).toBe(false);
  expect(needsAttention([app()])).toBe(false);
  expect(
    needsAttention([app({ lastDelivery: { status: "expired", reason: "", at, test: false } })]),
  ).toBe(false);
  expect(
    needsAttention([app({ lastDelivery: { status: "delivered", reason: "", at, test: true } })]),
  ).toBe(false);
  const failed = { status: "failed", reason: "closed", at, test: false } as const;
  expect(needsAttention([app(), app({ id: "c2", lastDelivery: failed })])).toBe(true);
  expect(needsAttention([app({ lastDelivery: failed, remind: false })])).toBe(false);
  expect(needsAttention([app({ lastDelivery: failed, enabled: false })])).toBe(false);
});

test("the access choice maps onto what the hub keeps", () => {
  expect(accessOf(app({ enabled: false }))).toBe("none");
  expect(accessOf(app({ remind: false }))).toBe("use");
  expect(accessOf(app())).toBe("remind");
  expect(accessOf(app({ intents: ["items"] }))).toBe("use");
  expect(grantFor("none")).toEqual({ enabled: false });
  expect(grantFor("use")).toEqual({ enabled: true, remind: false });
  expect(grantFor("remind")).toEqual({ enabled: true, remind: true });
  expect(levelsOf(app())).toEqual(["none", "use", "remind"]);
  expect(levelsOf(app({ intents: ["items"] }))).toEqual(["none", "use"]);
});

test("the status line follows the last delivery and says when it was a test", () => {
  expect(statusOf(app())).toEqual({ tone: "soft", key: "apps.statusNone" });
  expect(
    statusOf(app({ lastDelivery: { status: "pending", reason: "", at, test: false } })),
  ).toEqual({
    tone: "soft",
    key: "apps.statusPending",
  });
  expect(
    statusOf(app({ lastDelivery: { status: "delivered", reason: "", at, test: false } })),
  ).toEqual({
    tone: "ok",
    key: "apps.statusDelivered",
    at,
  });
  expect(
    statusOf(app({ lastDelivery: { status: "delivered", reason: "", at, test: true } })),
  ).toEqual({
    tone: "ok",
    key: "apps.statusTestDelivered",
    at,
  });
  expect(
    statusOf(app({ lastDelivery: { status: "failed", reason: "closed", at, test: true } })),
  ).toEqual({
    tone: "bad",
    key: "apps.statusTestFailed",
    reason: "closed",
  });
  expect(
    statusOf(app({ lastDelivery: { status: "expired", reason: "", at, test: false } })),
  ).toEqual({
    tone: "soft",
    key: "apps.statusExpired",
  });
});

test("apps are grouped by their kind, named after the provider", () => {
  const methods: LoginMethod[] = [
    { key: "discord", type: "discord", name: "Discord", login: true, link: true },
    { key: "gh", type: "github", name: "GitHub", login: true, link: false },
  ];
  const sections = sectionsOf(
    [
      app({ id: "b", name: "Team", provider: "discord" }),
      app({ id: "x", name: "Script", provider: "example" }),
      app({ id: "a", name: "plsremind", provider: "discord" }),
      app({ id: "g", name: "Hub", provider: "gh" }),
    ],
    methods,
  );
  expect(
    sections.map((s) => [s.provider, s.name, s.linkable, s.apps.map((one) => one.id)]),
  ).toEqual([
    ["discord", "Discord", true, ["a", "b"]],
    ["example", "example", false, ["x"]],
    ["gh", "GitHub", false, ["g"]],
  ]);
});
