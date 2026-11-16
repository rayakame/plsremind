import { dictionaries } from "../i18n/index.js";
import { addDays, isDay, weekdayOf } from "./zone.js";

/** A day, and a time of day unless the whole day is meant. */
export interface When {
  day: string;
  time: string | null;
}

const pad = (value: string | number) => String(value).padStart(2, "0");

function timeOf(text: string): string | null {
  const match = /^(\d{1,2}):(\d{2})$/.exec(text);
  if (!match || +match[1]! > 23 || +match[2]! > 59) return null;
  return `${pad(match[1]!)}:${match[2]}`;
}

function dayOf(text: string, today: string): string | null {
  const word = text.toLowerCase();
  for (const texts of dictionaries()) {
    if (word === texts["when.today"]) return today;
    if (word === texts["when.tomorrow"]) return addDays(today, 1);
    for (const names of [texts["when.weekdays"], texts["when.weekdaysShort"]]) {
      const weekday = names.split(",").indexOf(word);
      if (weekday >= 0) return addDays(today, ((weekday - weekdayOf(today) + 6) % 7) + 1);
    }
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(word)) return isDay(word) ? word : null;
  const dotted = /^(\d{1,2})\.(\d{1,2})\.(\d{4})?$/.exec(word);
  if (!dotted) return null;
  const rest = `${pad(dotted[2]!)}-${pad(dotted[1]!)}`;
  if (dotted[3]) return isDay(`${dotted[3]}-${rest}`) ? `${dotted[3]}-${rest}` : null;
  const year = +today.slice(0, 4);
  for (const candidate of [`${year}-${rest}`, `${year + 1}-${rest}`]) {
    if (isDay(candidate) && candidate >= today) return candidate;
  }
  return null;
}

/** Reads "tomorrow 12:00", "fri", "13.11." or "2026-11-13", in English and the chosen language. */
export function parseWhen(text: string, today: string): When | null {
  const words = text.trim().split(/\s+/);
  if (words.length > 2 || words[0] === "") return null;
  const day = dayOf(words[0]!, today);
  if (!day) return null;
  if (words.length === 1) return { day, time: null };
  const time = timeOf(words[1]!);
  return time ? { day, time } : null;
}

/** Takes a time expression off the end of a text, if one is there and something remains. */
export function splitTrailingWhen(
  text: string,
  today: string,
): { rest: string; when: When | null } {
  const words = text.trim().split(/\s+/);
  for (const take of [2, 1]) {
    if (words.length <= take) continue;
    const when = parseWhen(words.slice(-take).join(" "), today);
    if (when) return { rest: words.slice(0, -take).join(" "), when };
  }
  return { rest: text.trim(), when: null };
}
