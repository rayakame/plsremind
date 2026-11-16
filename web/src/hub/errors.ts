import { ApiError, NetworkError } from "plsremind";
import { t, type Key } from "../i18n/index.js";
import { en } from "../i18n/en.js";

/** The text for a hub error code, or undefined for a code without a translation. */
export function describeCode(code: string): string | undefined {
  const key = `errors.${code}`;
  return key in en ? t(key as Key) : undefined;
}

/** What to tell the user about a failed call, in their language where the hub's code is known. */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    const known = describeCode(error.code);
    if (error.code === "validation_failed" && error.fields) {
      return `${known} ${Object.values(error.fields).join(" ")}`;
    }
    return known ?? error.message;
  }
  if (error instanceof NetworkError) return t("errors.network");
  return t("errors.unexpected");
}

export function isSignedOut(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401 && error.code === "unauthorized";
}
