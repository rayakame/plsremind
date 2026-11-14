import { formatDuration, parseDuration } from "./duration.js";

const instants = new Set([
  "created_at",
  "updated_at",
  "due_at",
  "completed_at",
  "remind_at",
  "scheduled_for",
  "fire_at",
  "fired_at",
  "expires_at",
  "used_at",
  "last_used_at",
]);

const camelNames = new Map<string, string>();
const snakeNames = new Map<string, string>();

function camel(name: string): string {
  let out = camelNames.get(name);
  if (out === undefined) {
    out = name.replace(/_+([a-z0-9])/g, (_, letter: string) => letter.toUpperCase());
    camelNames.set(name, out);
  }
  return out;
}

function snake(name: string): string {
  let out = snakeNames.get(name);
  if (out === undefined) {
    out = name.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
    snakeNames.set(name, out);
  }
  return out;
}

/** Turns a value in the API's form into the SDK's: camelCase names, Dates, milliseconds. */
export function fromWire<T = unknown>(value: unknown): T {
  return read(value) as T;
}

function read(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(read);
  if (value === null || typeof value !== "object") return value;
  const out: Record<string, unknown> = {};
  for (const [key, field] of Object.entries(value)) {
    out[camel(key)] = readField(key, field);
  }
  return out;
}

function readField(key: string, field: unknown): unknown {
  if (key === "preferences") return field;
  if (typeof field === "string") {
    if (instants.has(key)) return new Date(field);
    if (key === "offset") return parseDuration(field);
    return field;
  }
  if (key === "before" && Array.isArray(field) && field.every((part) => typeof part === "string")) {
    return field.map(parseDuration);
  }
  return read(field);
}

/** The opposite of fromWire, for request bodies. Fields that are undefined are left out. */
export function toWire(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(toWire);
  if (value instanceof Date) return value.toISOString();
  if (value === null || typeof value !== "object") return value;
  const out: Record<string, unknown> = {};
  for (const [key, field] of Object.entries(value)) {
    if (field === undefined) continue;
    out[snake(key)] = writeField(key, field);
  }
  return out;
}

function writeField(key: string, field: unknown): unknown {
  if (key === "preferences") return field;
  if ((key === "in" || key === "expiresIn") && typeof field === "number") {
    return formatDuration(field);
  }
  if (key === "before" && Array.isArray(field) && field.every((part) => typeof part === "number")) {
    return field.map(formatDuration);
  }
  return toWire(field);
}

const instant = /^(.*?)(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/i;

/** Microseconds since 1970 of an RFC 3339 instant, which a Date would round to milliseconds. */
export function microsOf(text: string): bigint {
  const match = instant.exec(text);
  if (!match) throw new RangeError(`not an instant: ${JSON.stringify(text)}`);
  const seconds = Date.parse(`${match[1]}${match[3]}`);
  if (Number.isNaN(seconds)) throw new RangeError(`not an instant: ${JSON.stringify(text)}`);
  const fraction = (match[2] ?? "").slice(0, 6).padEnd(6, "0");
  return BigInt(seconds) * 1000n + BigInt(fraction);
}
