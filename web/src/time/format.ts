import type { Item } from "plsremind";
import { locale, t } from "../i18n/index.js";
import { addDays, dayIn } from "./zone.js";

function noon(day: string): Date {
  return new Date(`${day}T12:00:00Z`);
}

/** A day as short text: "today", "Fri", "Dec 24" or, in another year, "Jan 2, 2027". */
export function formatDay(day: string, today: string): string {
  if (day === today) return t("when.today");
  if (day === addDays(today, 1)) return t("when.tomorrow");
  if (day === addDays(today, -1)) return t("when.yesterday");
  if (day > today && day <= addDays(today, 6)) {
    return new Intl.DateTimeFormat(locale(), { weekday: "short", timeZone: "UTC" }).format(
      noon(day),
    );
  }
  return new Intl.DateTimeFormat(locale(), {
    day: "numeric",
    month: "short",
    year: day.slice(0, 4) === today.slice(0, 4) ? undefined : "numeric",
    timeZone: "UTC",
  }).format(noon(day));
}

const clocks = new Map<string, Intl.DateTimeFormat>();

/** A time of day the way the user's language writes it: "21:00" or "9:00 PM". */
export function formatTime(instant: Date, zone: string): string {
  const key = `${locale()}|${zone}`;
  let clock = clocks.get(key);
  if (!clock) {
    clock = new Intl.DateTimeFormat(locale(), { timeStyle: "short", timeZone: zone });
    clocks.set(key, clock);
  }
  // Engines disagree on the kind of space before AM and PM.
  return clock.format(instant).replace(/\s/g, " ");
}

/** A moment as "tomorrow 19:00" or "Fri 08:00". */
export function formatMoment(instant: Date, zone: string, today: string): string {
  return `${formatDay(dayIn(instant, zone), today)} ${formatTime(instant, zone)}`;
}

export function formatDue(
  item: Pick<Item, "dueAt" | "dueDate" | "allDay">,
  zone: string,
  today: string,
): string {
  if (item.allDay && item.dueDate) return formatDay(item.dueDate, today);
  if (!item.dueAt) return "";
  return formatMoment(item.dueAt, zone, today);
}

/** A full date and time for places with room, in the user's language. */
export function formatInstant(instant: Date, zone: string): string {
  return new Intl.DateTimeFormat(locale(), {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: zone,
  }).format(instant);
}
