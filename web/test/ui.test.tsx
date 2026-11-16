import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { Button, Field, Input, Segmented, Toggle } from "../src/ui/controls.js";
import { Dialog } from "../src/ui/dialog.js";

afterEach(cleanup);

test("a button is a real button and carries its variant", () => {
  render(<Button variant="primary">Create</Button>);
  const button = screen.getByRole("button", { name: "Create" });
  expect(button.getAttribute("type")).toBe("button");
  expect(button.className).toBe("btn primary");
});

test("a field names its control and shows its error", () => {
  render(
    <Field label="Title" error="Too long">
      <Input defaultValue="x" />
    </Field>,
  );
  const input = screen.getByLabelText("Title");
  expect(input.tagName).toBe("INPUT");
  expect(input.getAttribute("aria-invalid")).toBe("true");
  expect(screen.getByText("Too long")).toBeTruthy();
});

test("a toggle is a checkbox found by its label", () => {
  const changed = vi.fn();
  render(<Toggle label="Remind" checked={false} onChange={changed} />);
  fireEvent.click(screen.getByRole("checkbox", { name: "Remind" }));
  expect(changed).toHaveBeenCalledWith(true);
});

test("a segmented control marks the chosen option", () => {
  const changed = vi.fn();
  render(
    <Segmented
      label="View"
      value="month"
      options={[
        { value: "month", label: "Month" },
        { value: "week", label: "Week" },
      ]}
      onChange={changed}
    />,
  );
  expect(screen.getByRole("button", { name: "Month" }).getAttribute("aria-pressed")).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "Week" }));
  expect(changed).toHaveBeenCalledWith("week");
});

test("a dialog is modal, closes on Escape and gives the focus back", () => {
  const closed = vi.fn();
  function Page({ open }: { open: boolean }) {
    return (
      <>
        <button>opener</button>
        {open && (
          <Dialog title="Delete list" onClose={closed}>
            <button>inside</button>
          </Dialog>
        )}
      </>
    );
  }
  const { rerender } = render(<Page open={false} />);
  const opener = screen.getByRole("button", { name: "opener" });
  opener.focus();
  rerender(<Page open />);
  const dialog = screen.getByRole("dialog", { name: "Delete list" });
  expect(dialog.getAttribute("aria-modal")).toBe("true");
  expect(dialog.contains(document.activeElement)).toBe(true);
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(closed).toHaveBeenCalledOnce();
  rerender(<Page open={false} />);
  expect(document.activeElement).toBe(opener);
});

test("a dialog without anything to tab to keeps the focus and still closes on Escape", () => {
  const closed = vi.fn();
  render(
    <>
      <button>outside</button>
      <Dialog title="Keys" onClose={closed}>
        <p>only text</p>
      </Dialog>
    </>,
  );
  const dialog = screen.getByRole("dialog", { name: "Keys" });
  expect(document.activeElement).toBe(dialog);
  for (const shiftKey of [false, true]) {
    const tab = new KeyboardEvent("keydown", {
      key: "Tab",
      shiftKey,
      bubbles: true,
      cancelable: true,
    });
    dialog.dispatchEvent(tab);
    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(dialog);
  }
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(closed).toHaveBeenCalledOnce();
});

test("a dialog whose opener was disabled meanwhile gives the focus to something near it", () => {
  function Page({ open, busy }: { open: boolean; busy: boolean }) {
    return (
      <fieldset>
        <button disabled={busy}>opener</button>
        <button>neighbour</button>
        {open && (
          <Dialog title="Rename" onClose={() => {}}>
            <button>inside</button>
          </Dialog>
        )}
      </fieldset>
    );
  }
  const { rerender } = render(<Page open={false} busy={false} />);
  screen.getByRole("button", { name: "opener" }).focus();
  rerender(<Page open busy={false} />);
  rerender(<Page open busy />);
  rerender(<Page open={false} busy />);
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "neighbour" }));
});

test("Tab comes back into a dialog when the focus fell out of it", () => {
  render(
    <>
      <button>outside</button>
      <Dialog title="Reset" onClose={() => {}}>
        <button>inside</button>
      </Dialog>
    </>,
  );
  (document.activeElement as HTMLElement).blur();
  expect(document.activeElement).toBe(document.body);
  const tab = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
  document.body.dispatchEvent(tab);
  expect(tab.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "inside" }));
});
