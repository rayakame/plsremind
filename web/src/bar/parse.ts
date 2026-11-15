import type { List, Priority } from "plsremind";
import { dictionaries } from "../i18n/index.js";
import { splitTrailingWhen, type When } from "../time/when.js";

export interface NewDraft {
  title: string;
  when: When | null;
  priority: Priority | null;
  list: List | null;
  /** What was typed after "#" when no list, or more than one, starts with it. */
  unknownList: string | null;
}

const priorities: readonly Priority[] = ["low", "normal", "high"];

/** A priority by its English name or its name in the chosen language. */
export function priorityOf(word: string): Priority | null {
  const wanted = word.toLowerCase();
  for (const priority of priorities) {
    if (wanted === priority) return priority;
    for (const texts of dictionaries()) {
      if (wanted === texts[`priority.${priority}`].toLowerCase()) return priority;
    }
  }
  return null;
}

/** The list whose name is this text, or the only one whose name starts with it. */
export function listOf(text: string, lists: readonly List[]): List | null {
  const wanted = text.replace(/^#/, "").toLowerCase();
  if (wanted === "") return null;
  const exact = lists.filter((list) => list.name.toLowerCase() === wanted);
  if (exact.length === 1) return exact[0]!;
  const starting = lists.filter((list) => list.name.toLowerCase().startsWith(wanted));
  return starting.length === 1 ? starting[0]! : null;
}

/** Reads "!priority", "#list" and a time at the end out of a new item's text. */
export function parseNew(text: string, ctx: { today: string; lists: readonly List[] }): NewDraft {
  let priority: Priority | null = null;
  let list: List | null = null;
  let unknownList: string | null = null;
  const kept: string[] = [];
  for (const word of text.trim().split(/\s+/).filter(Boolean)) {
    if (word.startsWith("!") && word.length > 1) {
      const found = priorityOf(word.slice(1));
      if (found && !priority) {
        priority = found;
        continue;
      }
    } else if (word.startsWith("#") && word.length > 1) {
      const found = listOf(word, ctx.lists);
      if (found && !list) {
        list = found;
        continue;
      }
      if (!found) unknownList ??= word.slice(1);
    }
    kept.push(word);
  }
  const { rest, when } = splitTrailingWhen(kept.join(" "), ctx.today);
  return { title: rest, when, priority, list, unknownList };
}

/** Splits a command into its name and the rest, or null if the text is not a command. */
export function parseCommand(text: string, prefix: string): { name: string; rest: string } | null {
  if (!text.startsWith(prefix)) return null;
  const body = text.slice(prefix.length);
  const space = body.search(/\s/);
  if (space < 0) return { name: body.toLowerCase(), rest: "" };
  return { name: body.slice(0, space).toLowerCase(), rest: body.slice(space).trim() };
}

/** The text with the command's name finished, if exactly a name is being typed. */
export function complete(text: string, prefix: string, names: readonly string[]): string | null {
  const command = parseCommand(text, prefix);
  if (!command || /\s/.test(text)) return null;
  const found = names.find((name) => name.startsWith(command.name));
  return found ? `${prefix}${found} ` : null;
}
