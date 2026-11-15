const formats = new Map<string, Intl.DateTimeFormat>();

function formatOf(zone: string): Intl.DateTimeFormat {
  let format = formats.get(zone);
  if (!format) {
    format = new Intl.DateTimeFormat("en-CA", {
      timeZone: zone,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
    formats.set(zone, format);
  }
  return format;
}

function partsIn(instant: Date, zone: string): Record<string, string> {
  const parts: Record<string, string> = {};
  for (const part of formatOf(zone).formatToParts(instant)) parts[part.type] = part.value;
  return parts;
}

function offsetAt(instant: number, zone: string): number {
  const p = partsIn(new Date(instant), zone);
  const local = Date.UTC(+p.year!, +p.month! - 1, +p.day!, +p.hour!, +p.minute!, +p.second!);
  return local - Math.floor(instant / 1000) * 1000;
}

function utc(day: string): number {
  const [year, month, date] = day.split("-").map(Number);
  return Date.UTC(year!, month! - 1, date!);
}

function dayOf(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/** True for a zone name the browser knows. */
export function isZone(zone: string): boolean {
  try {
    formatOf(zone);
    return true;
  } catch {
    return false;
  }
}

/** The calendar day of an instant in a zone, as "YYYY-MM-DD". */
export function dayIn(instant: Date, zone: string): string {
  const p = partsIn(instant, zone);
  return `${p.year}-${p.month}-${p.day}`;
}

/** The time of day of an instant in a zone, as "HH:MM". */
export function timeIn(instant: Date, zone: string): string {
  const p = partsIn(instant, zone);
  return `${p.hour}:${p.minute}`;
}

/** The instant at which a zone's clocks show the given day and time. */
export function instantAt(day: string, time: string, zone: string): Date {
  const [hour, minute] = time.split(":").map(Number);
  const guess = utc(day) + (hour! * 60 + minute!) * 60_000;
  const first = guess - offsetAt(guess, zone);
  return new Date(guess - offsetAt(first, zone));
}

export function addDays(day: string, days: number): string {
  return dayOf(utc(day) + days * 86_400_000);
}

/** 0 for Monday up to 6 for Sunday. */
export function weekdayOf(day: string): number {
  return (new Date(utc(day)).getUTCDay() + 6) % 7;
}

export function weekStartOf(day: string, weekStart: "monday" | "sunday"): string {
  const back = weekStart === "monday" ? weekdayOf(day) : (weekdayOf(day) + 1) % 7;
  return addDays(day, -back);
}

/** The 42 days of the six weeks that hold a month given as "YYYY-MM". */
export function monthGrid(month: string, weekStart: "monday" | "sunday"): string[] {
  const start = weekStartOf(`${month}-01`, weekStart);
  return Array.from({ length: 42 }, (_, i) => addDays(start, i));
}

export function isDay(day: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(day) && dayOf(utc(day)) === day;
}
