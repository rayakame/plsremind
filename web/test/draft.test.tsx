import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";
import { useDraft } from "../src/ui/draft.js";
import { deferred } from "./support.js";

afterEach(cleanup);

function Field(props: { save: (next: string) => Promise<boolean> }) {
  return <input aria-label="Name" {...useDraft("A", props.save)} />;
}

test("a save that fails late does not undo a newer edit that is still on its way", async () => {
  const saves = [deferred<boolean>(), deferred<boolean>()];
  let asked = 0;
  render(<Field save={() => saves[asked++]!.promise} />);
  const field = screen.getByLabelText("Name") as HTMLInputElement;
  await userEvent.clear(field);
  await userEvent.type(field, "B");
  await userEvent.tab();
  await userEvent.clear(field);
  await userEvent.type(field, "C");
  await userEvent.tab();
  await act(async () => {
    saves[0]!.resolve(false);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(field.value).toBe("C");
  await act(async () => {
    saves[1]!.resolve(false);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(field.value).toBe("A");
});
