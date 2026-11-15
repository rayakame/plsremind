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
