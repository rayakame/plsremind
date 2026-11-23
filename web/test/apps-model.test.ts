import type { UserConnection } from "plsremind";
import { expect, test } from "vitest";
import { needsAttention } from "../src/pages/settings/apps-model.js";

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
