import { localPath } from "../shell/router.js";

const originKey = "plsremind.linkFrom";
// Linking that was abandoned must not steer a later one.
const originFor = 10 * 60_000;

/** Remembers the page a link to an account elsewhere starts from, to come back to it. */
export function rememberLinkOrigin(path: string): void {
  try {
    sessionStorage.setItem(originKey, `${Date.now()} ${path}`);
  } catch {
    // The confirmation then offers the settings instead.
  }
}

/** The page the last link started from, once. */
export function takeLinkOrigin(): string | null {
  try {
    const stored = sessionStorage.getItem(originKey);
    sessionStorage.removeItem(originKey);
    if (!stored) return null;
    const at = stored.indexOf(" ");
    if (Date.now() - Number(stored.slice(0, at)) >= originFor) return null;
    const path = localPath(stored.slice(at + 1));
    return path === "/" ? null : path;
  } catch {
    return null;
  }
}
