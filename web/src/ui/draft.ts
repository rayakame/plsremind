import { useEffect, useRef, useState, type ChangeEvent, type KeyboardEvent } from "react";

/**
 * A field that saves when it is left, not on every key or every step of a date picker. While
 * it has the focus a newer value from outside does not replace what is being typed.
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
    if (focused.current) return;
    setText(value);
    base.current = value;
  }, [value]);

  const commit = () => {
    const next = options.required ? text.trim() : text;
    if (next === base.current) return false;
    if (options.required && next === "") return false;
    base.current = next;
    save(next);
    return true;
  };

  // A field that is taken off the page while it has the focus gets no blur.
  const last = useRef(commit);
  last.current = commit;
  useEffect(
    () => () => {
      if (focused.current) last.current();
    },
    [],
  );

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

/**
 * A draft field whose text has to mean something. What cannot be read is not saved and
 * marks the field for as long as it shows that text.
 */
export function useCheckedDraft<T>(
  value: string,
  read: (text: string) => T | undefined,
  save: (parsed: T) => void,
  options: { required?: boolean; enter?: boolean } = {},
) {
  const [refused, setRefused] = useState<string>();
  const field = useDraft(
    value,
    (next) => {
      const parsed = read(next);
      setRefused(parsed === undefined ? next : undefined);
      if (parsed !== undefined) save(parsed);
    },
    options,
  );
  return { invalid: refused !== undefined && field.value === refused, field };
}
