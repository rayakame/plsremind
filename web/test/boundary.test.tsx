import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { Boundary } from "../src/shell/boundary.js";

afterEach(cleanup);

function Broken(): never {
  throw new Error("a chunk that is gone");
}

test("a page that fails to load offers a reload instead of a blank screen", () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  render(
    <Boundary>
      <Broken />
    </Boundary>,
  );
  expect(screen.getByRole("heading", { name: "Something went wrong." })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
});
