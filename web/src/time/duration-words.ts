import { t } from "../i18n/index.js";

const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;

/** Joins parts the way people say a list: "a, b and c". */
export function listWords(parts: readonly string[]): string {
  if (parts.length <= 1) return parts[0] ?? "";
  return t("duration.list", { first: parts.slice(0, -1).join(", "), last: parts.at(-1)! });
}

/** A duration in words, such as "1 day and 2 hours". */
export function durationWords(ms: number): string {
  const days = Math.floor(ms / day);
  const hours = Math.floor((ms % day) / hour);
  const minutes = Math.floor((ms % hour) / minute);
  const parts: string[] = [];
  if (days > 0) parts.push(t(days === 1 ? "duration.day" : "duration.days", { count: days }));
  if (hours > 0) parts.push(t(hours === 1 ? "duration.hour" : "duration.hours", { count: hours }));
  if (minutes > 0) {
    parts.push(t(minutes === 1 ? "duration.minute" : "duration.minutes", { count: minutes }));
  }
  return listWords(parts);
}
