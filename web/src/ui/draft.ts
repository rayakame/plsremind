import { useEffect, useRef, useState, type ChangeEvent, type KeyboardEvent } from "react";

/**
 * A text field that saves when it is left, not on every key. While it has the focus a newer
 * value from outside does not replace what is being typed.
 */
export function useDraft(
  value: string,
  save: (next: string) => void,
  options: { required?: boolean; enter?: boolean } = {},
) {
  const [text, setText] = useState(value);
  const focused = useRef(false);
  const base = useRef(value);

  useEffect(() => {
    if (!focused.current) setText(value);
  }, [value]);

  const commit = () => {
    const next = options.required ? text.trim() : text;
    if (next === base.current) return false;
    if (options.required && next === "") return false;
    base.current = next;
    save(next);
    return true;
  };

  return {
    value: text,
    onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      setText(event.target.value),
    onFocus: () => {
      focused.current = true;
      base.current = value;
    },
    onBlur: () => {
      focused.current = false;
      if (!commit()) setText(value);
    },
    onKeyDown: (event: KeyboardEvent) => {
      if (options.enter && event.key === "Enter") commit();
    },
  };
}
