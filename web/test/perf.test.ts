// @vitest-environment node
import { expect, test } from "vitest";
import { openCounts } from "../src/model/counts.js";
import { groupOpen, nowIn } from "../src/model/groups.js";
import { filterItems } from "../src/model/search.js";
import { many } from "./items.js";

function median(run: () => void): number {
  run();
  const times = Array.from({ length: 5 }, () => {
    const start = performance.now();
    run();
    return performance.now() - start;
  });
  return times.sort((a, b) => a - b)[2]!;
}

test("grouping, counting and searching 5000 items each stay under 50 ms", () => {
  const now = nowIn("Europe/Berlin", "monday", new Date("2026-11-15T12:00:00Z"));
  let round = 0;
  const fresh = () => many(5000).map((one) => ({ ...one, title: `${one.title} ${round++}` }));
  expect(median(() => groupOpen(fresh(), now, "due"))).toBeLessThan(50);
  expect(median(() => groupOpen(fresh(), now, "priority"))).toBeLessThan(50);
  expect(median(() => openCounts(fresh()))).toBeLessThan(50);
  expect(median(() => filterItems(fresh(), "report uber"))).toBeLessThan(50);
  const items = fresh();
  filterItems(items, "x");
  expect(median(() => filterItems(items, `call ${round++}`))).toBeLessThan(20);
});
