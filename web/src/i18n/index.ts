import { useMemo, useSyncExternalStore } from "react";
import { de } from "./de.js";
import { en, type Key, type Texts } from "./en.js";

export type { Key, Texts };

export const languages: Readonly<Record<string, Texts>> = { en, de };

let tag = "en";
let active: Texts = en;
const listeners = new Set<() => void>();

/** Chooses the language for a tag such as "de-AT". An unknown one falls back to English. */
export function setLocale(wanted: string | null): void {
  const found = wanted ? languages[wanted.toLowerCase().split("-")[0]!] : undefined;
  const next = found && wanted ? wanted : "en";
  if (next === tag) return;
  tag = next;
  active = found ?? en;
  for (const listener of [...listeners]) listener();
}

/** The tag to format dates and numbers with. */
export function locale(): string {
  return tag;
}

export function t(key: Key, params?: Record<string, string | number>): string {
  const text = active[key];
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (hole, name: string) =>
    name in params ? String(params[name]) : hole,
  );
}

/** English and, if it is another one, the chosen language. */
export function dictionaries(): readonly Texts[] {
  return active === en ? [en] : [en, active];
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Makes a component render again when the language changes. The function it returns is a
 * new one per language, so that it can stand in a dependency list.
 */
export function useT(): typeof t {
  const tag = useSyncExternalStore(subscribe, locale);
  return useMemo(() => ((key, params) => t(key, params)) as typeof t, [tag]);
}
