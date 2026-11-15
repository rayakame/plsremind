import type { Item } from "plsremind";
import { locale, t } from "../i18n/index.js";
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

export function formatDue(
  item: Pick<Item, "dueAt" | "dueDate" | "allDay">,
  zone: string,
  today: string,
): string {
  if (item.allDay && item.dueDate) return formatDay(item.dueDate, today);
  if (!item.dueAt) return "";
  return `${formatDay(dayIn(item.dueAt, zone), today)} ${timeIn(item.dueAt, zone)}`;
}

/** A full date and time for places with room, in the user's language. */
export function formatInstant(instant: Date, zone: string): string {
  return new Intl.DateTimeFormat(locale(), {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: zone,
  }).format(instant);
}
