import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import { useLocation } from "wouter";
import { useProfile } from "../hub/hooks.js";
import { readPrefs } from "../hub/prefs.js";

interface Bar {
  text: string;
  setText: (text: string) => void;
  /** What the view should be narrowed to; empty while a command is being typed. */
  filter: string;
  prefix: string;
  focus: (text?: string) => void;
  clear: () => void;
  /** The day new items are due on, set by the calendar. */
  defaultDue: string | null;
  setDefaultDue: (day: string | null) => void;
  input: RefObject<HTMLInputElement | null>;
}

const BarContext = createContext<Bar | undefined>(undefined);

export function BarProvider(props: { children: ReactNode }) {
  const profile = useProfile();
  const { prefix } = readPrefs(profile.preferences);
  const [text, setText] = useState("");
  const [defaultDue, setDefaultDue] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const [path] = useLocation();

  useEffect(() => setText(""), [path]);

  const focus = useCallback((next?: string) => {
    if (next !== undefined) setText(next);
    input.current?.focus();
  }, []);
  const clear = useCallback(() => {
    setText("");
    input.current?.blur();
  }, []);

  const value = useMemo(
    () => ({
      text,
      setText,
      filter: text.startsWith(prefix) ? "" : text,
      prefix,
      focus,
      clear,
      defaultDue,
      setDefaultDue,
      input,
    }),
    [text, prefix, focus, clear, defaultDue],
  );
  return <BarContext.Provider value={value}>{props.children}</BarContext.Provider>;
}

export function useBar(): Bar {
  const bar = useContext(BarContext);
  if (!bar) throw new Error("no bar");
  return bar;
}
