import type { UserConnection } from "plsremind";

/** True when an app that reminds the user could not deliver its last reminder. */
export function needsAttention(apps: readonly UserConnection[]): boolean {
  return apps.some((app) => app.enabled && app.remind && app.lastDelivery?.status === "failed");
}
