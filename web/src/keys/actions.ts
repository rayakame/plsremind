export const actions = [
  "down",
  "up",
  "open",
  "complete",
  "new",
  "find",
  "remind",
  "due",
  "move",
  "goto",
  "theme",
  "help",
] as const;

export type ActionId = (typeof actions)[number];

export const defaults: Readonly<Record<ActionId, string>> = {
  down: "j",
  up: "k",
  open: "Enter",
  complete: "x",
  new: "n",
  find: "f",
  remind: "r",
  due: "d",
  move: "m",
  goto: "g",
  theme: "t",
  help: "?",
};

/** The key of every action, with the user's own choices over the defaults. */
export function keysOf(overrides: Readonly<Record<string, string>>): Record<ActionId, string> {
  const keys = { ...defaults };
  for (const action of actions) {
    const own = overrides[action];
    if (own) keys[action] = own;
  }
  return keys;
}

/** Which action a key triggers. */
export function bindings(
  overrides: Readonly<Record<string, string>>,
): ReadonlyMap<string, ActionId> {
  const keys = keysOf(overrides);
  const map = new Map<string, ActionId>();
  for (const action of actions) {
    if (!map.has(keys[action])) map.set(keys[action], action);
  }
  return map;
}

export type Rebound =
  { ok: true; overrides: Record<string, string> } | { ok: false; conflict: ActionId | null };

/** Gives an action another key, unless another action or the command prefix already has it. */
export function rebind(
  overrides: Readonly<Record<string, string>>,
  action: ActionId,
  key: string,
  prefix = "",
): Rebound {
  if (key === "" || key === " " || key === prefix) return { ok: false, conflict: null };
  const other = bindings(overrides).get(key);
  if (other !== undefined && other !== action) return { ok: false, conflict: other };
  const next = { ...overrides };
  if (key === defaults[action]) delete next[action];
  else next[action] = key;
  return { ok: true, overrides: next };
}
