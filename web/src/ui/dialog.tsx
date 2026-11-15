import { useEffect, useId, useRef, type KeyboardEvent, type ReactNode } from "react";

const focusable =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function Dialog(props: {
  title: string;
  onClose: () => void;
  wide?: boolean | undefined;
  /** Closes only through its own buttons, for content that cannot be shown again. */
  sticky?: boolean | undefined;
  children: ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const title = useId();

  useEffect(() => {
    const before = document.activeElement;
    const first = box.current?.querySelector<HTMLElement>(focusable);
    (first ?? box.current)?.focus();
    return () => {
      if (before instanceof HTMLElement) before.focus();
    };
  }, []);

  function onKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.stopPropagation();
      if (!props.sticky) props.onClose();
      return;
    }
    if (event.key !== "Tab" || !box.current) return;
    const all = [...box.current.querySelectorAll<HTMLElement>(focusable)];
    const first = all[0];
    const last = all.at(-1);
    if (!first || !last) {
      event.preventDefault();
      box.current.focus();
      return;
    }
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div
      className="scrim"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !props.sticky) props.onClose();
      }}
    >
      <div
        ref={box}
        className={props.wide ? "dialog wide" : "dialog"}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        <h2 className="h2" id={title}>
          {props.title}
        </h2>
        {props.children}
      </div>
    </div>
  );
}
