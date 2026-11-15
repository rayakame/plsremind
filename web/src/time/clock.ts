import { locale } from "../i18n/index.js";

export type ClockChoice = "auto" | "12h" | "24h";

const pad = (value: number) => String(value).padStart(2, "0");

/** True if times are written with AM and PM: by the user's choice, else by the language. */
export function usesHour12(choice: ClockChoice): boolean {
  if (choice !== "auto") return choice === "12h";
  return new Intl.DateTimeFormat(locale(), { hour: "numeric" }).resolvedOptions().hour12 === true;
}

/** Reads "18:00", "6 pm" or "6.30" as "HH:MM". Empty text stays empty; anything else is undefined. */
export function parseClock(text: string): string | undefined {
  const typed = text.trim().toLowerCase();
  if (typed === "") return "";
  const match = /^(\d{1,2})(?:[:.](\d{2}))?\s*(a\.?m\.?|p\.?m\.?)?$/.exec(typed);
  if (!match) return undefined;
  let hour = Number(match[1]);
  const minute = Number(match[2] ?? 0);
  const half = match[3]?.[0];
  if (minute > 59) return undefined;
  if (half) {
    if (hour < 1 || hour > 12) return undefined;
    hour = (hour % 12) + (half === "p" ? 12 : 0);
  } else if (hour > 23) {
    return undefined;
  }
  return `${pad(hour)}:${pad(minute)}`;
}

/** Shows "HH:MM" on a 12 or 24 hour clock. */
export function showClock(time: string, hour12: boolean): string {
  if (time === "" || !hour12) return time;
  const [hour, minute] = time.split(":").map(Number);
  return `${hour! % 12 || 12}:${pad(minute!)} ${hour! < 12 ? "AM" : "PM"}`;
}
