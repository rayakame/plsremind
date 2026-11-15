import type { Item } from "plsremind";
import { locale, t } from "../i18n/index.js";
import { showClock } from "./clock.js";
import { addDays, dayIn, timeIn } from "./zone.js";

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

/** Where and how times are shown: the zone, today's date there, and the kind of clock. */
export interface Clock {
  readonly zone: string;
  readonly today: string;
  readonly hour12: boolean;
}

/** A time of day on the user's clock: "21:00" or "9:00 PM". */
export function formatTime(instant: Date, clock: Pick<Clock, "zone" | "hour12">): string {
  return showClock(timeIn(instant, clock.zone), clock.hour12);
}

/** A moment as "tomorrow 19:00" or "Fri 08:00". */
export function formatMoment(instant: Date, clock: Clock): string {
  return `${formatDay(dayIn(instant, clock.zone), clock.today)} ${formatTime(instant, clock)}`;
}

export function formatDue(item: Pick<Item, "dueAt" | "dueDate" | "allDay">, clock: Clock): string {
  if (item.allDay && item.dueDate) return formatDay(item.dueDate, clock.today);
  if (!item.dueAt) return "";
  return formatMoment(item.dueAt, clock);
}

/** A full date and time for places with room, in the user's language. */
export function formatInstant(instant: Date, zone: string): string {
  return new Intl.DateTimeFormat(locale(), {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: zone,
  }).format(instant);
}
