import { useSyncExternalStore } from "react";

function subscribe(notify: () => void): () => void {
  window.addEventListener("resize", notify);
  return () => window.removeEventListener("resize", notify);
}

function narrow(): boolean {
  const layout = getComputedStyle(document.documentElement).getPropertyValue("--layout");
  return layout.trim() === "narrow";
}

/** True at phone width, as the style file defines it. */
export function useNarrow(): boolean {
  return useSyncExternalStore(subscribe, narrow);
}

/** The height of a row in the style system, as a number for virtual lists. */
export function rowHeight(): number {
  const height = getComputedStyle(document.documentElement).getPropertyValue("--h");
  return Number.parseFloat(height) || 44;
}
