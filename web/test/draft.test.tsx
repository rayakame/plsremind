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

function Stored(props: { value: string; save: (next: string) => Promise<boolean> }) {
  return <input aria-label="Name" {...useDraft(props.value, props.save)} />;
}

test("an older save that succeeds does not replace a newer edit that is still on its way", async () => {
  const saves = [deferred<boolean>(), deferred<boolean>()];
  let asked = 0;
  const save = () => saves[asked++]!.promise;
  const { rerender } = render(<Stored value="A" save={save} />);
  const field = screen.getByLabelText("Name") as HTMLInputElement;
  await userEvent.clear(field);
  await userEvent.type(field, "B");
  await userEvent.tab();
  await userEvent.clear(field);
  await userEvent.type(field, "C");
  await userEvent.tab();
  await act(async () => {
    rerender(<Stored value="B" save={save} />);
    saves[0]!.resolve(true);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(field.value).toBe("C");
  await act(async () => {
    saves[1]!.resolve(false);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(field.value).toBe("B");
});

function Entered(props: { save: (next: string) => Promise<boolean> }) {
  return <input aria-label="Name" {...useDraft("A", props.save, { enter: true })} />;
}

test("leaving the field after Enter keeps what is being saved", async () => {
  const saving = deferred<boolean>();
  render(<Entered save={() => saving.promise} />);
  const field = screen.getByLabelText("Name") as HTMLInputElement;
  await userEvent.clear(field);
  await userEvent.type(field, "B{Enter}");
  await userEvent.tab();
  expect(field.value).toBe("B");
  await act(async () => {
    saving.resolve(false);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  expect(field.value).toBe("A");
});
