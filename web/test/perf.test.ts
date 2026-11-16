// @vitest-environment node
import { expect, test } from "vitest";
import { openCounts } from "../src/model/counts.js";
import { groupOpen, nowIn } from "../src/model/groups.js";
import { filterItems } from "../src/model/search.js";
import { many } from "./items.js";

// The fastest of several runs, since other test files compete for the processor.
function fastest<T>(prepare: () => T, run: (input: T) => void): number {
  run(prepare());
  const times = Array.from({ length: 7 }, () => {
    const input = prepare();
    const start = performance.now();
    run(input);
    return performance.now() - start;
  });
  return Math.min(...times);
}

test("grouping, counting and searching 5000 items each stay under 50 ms", () => {
  const now = nowIn("Europe/Berlin", "monday", new Date("2026-11-15T12:00:00Z"));
  let round = 0;
  const fresh = () => many(5000).map((one) => ({ ...one, title: `${one.title} ${round++}` }));
  expect(fastest(fresh, (items) => groupOpen(items, now, "due"))).toBeLessThan(50);
  expect(fastest(fresh, (items) => groupOpen(items, now, "priority"))).toBeLessThan(50);
  expect(fastest(fresh, (items) => openCounts(items))).toBeLessThan(50);
  expect(fastest(fresh, (items) => filterItems(items, "report uber"))).toBeLessThan(50);
  const items = fresh();
  filterItems(items, "x");
  expect(
    fastest(
      () => `call ${round++}`,
      (text) => filterItems(items, text),
    ),
  ).toBeLessThan(20);
});
