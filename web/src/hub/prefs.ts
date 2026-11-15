/** What the dashboard keeps in the profile's free preferences. */
export interface Prefs {
  theme: "system" | "light" | "dark";
  weekStart: "monday" | "sunday";
  prefix: string;
  /** Action to key, only where it differs from the default. */
  keys: Record<string, string>;
}

const themes = ["system", "light", "dark"];
const weekStarts = ["monday", "sunday"];

export function isPrefix(value: unknown): value is string {
  return typeof value === "string" && [...value].length === 1 && !/[\p{L}\p{N}\s]/u.test(value);
}

/** Reads preferences as stored, with the default for whatever is missing or malformed. */
export function readPrefs(preferences: unknown): Prefs {
  const stored = (
    preferences !== null && typeof preferences === "object" ? preferences : {}
  ) as Record<string, unknown>;
  const keys: Record<string, string> = {};
  if (stored.keys !== null && typeof stored.keys === "object" && !Array.isArray(stored.keys)) {
    for (const [action, key] of Object.entries(stored.keys)) {
      if (typeof key === "string" && key !== "") keys[action] = key;
    }
  }
  return {
    theme: themes.includes(stored.theme as string) ? (stored.theme as Prefs["theme"]) : "system",
    weekStart: weekStarts.includes(stored.weekStart as string)
      ? (stored.weekStart as Prefs["weekStart"])
      : "monday",
    prefix: isPrefix(stored.prefix) ? stored.prefix : "/",
    keys,
  };
}

/** The whole preferences object to store: the hub replaces it, so foreign keys are carried along. */
export function mergePrefs(preferences: unknown, patch: Partial<Prefs>): Record<string, unknown> {
  const stored = preferences !== null && typeof preferences === "object" ? preferences : {};
  return { ...stored, ...patch };
}
