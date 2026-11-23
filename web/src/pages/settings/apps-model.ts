import type { LoginMethod, UserConnection } from "plsremind";
import type { Key } from "../../i18n/index.js";

/** What a user lets an app do: nothing, act for them, or also remind them. */
export type Access = "none" | "use" | "remind";

const reminds = (app: UserConnection) => app.intents.includes("deliveries");

/** True when an app that reminds the user could not deliver its last reminder. */
export function needsAttention(apps: readonly UserConnection[]): boolean {
  return apps.some((app) => app.enabled && app.remind && app.lastDelivery?.status === "failed");
}

export function accessOf(app: UserConnection): Access {
  if (!app.enabled) return "none";
  return app.remind && reminds(app) ? "remind" : "use";
}

/** The grant that gives an app this access. Turning it off keeps the reminder choice for later. */
export function grantFor(access: Access): { enabled: boolean; remind?: boolean } {
  if (access === "none") return { enabled: false };
  return { enabled: true, remind: access === "remind" };
}

export function levelsOf(app: UserConnection): readonly Access[] {
  return reminds(app) ? ["none", "use", "remind"] : ["none", "use"];
}

export interface Status {
  readonly tone: "ok" | "soft" | "bad";
  readonly key: Key;
  readonly at?: Date;
  readonly reason?: string;
}

export function statusOf(app: UserConnection): Status {
  const last = app.lastDelivery;
  switch (last?.status) {
    case undefined:
      return { tone: "soft", key: "apps.statusNone" };
    case "pending":
      return { tone: "soft", key: "apps.statusPending" };
    case "delivered":
      return {
        tone: "ok",
        key: last.test ? "apps.statusTestDelivered" : "apps.statusDelivered",
        at: last.at,
      };
    case "failed":
      return {
        tone: "bad",
        key: last.test ? "apps.statusTestFailed" : "apps.statusFailed",
        reason: last.reason,
      };
    case "expired":
      return { tone: "soft", key: "apps.statusExpired" };
  }
}

export interface Section {
  readonly provider: string;
  readonly name: string;
  readonly linkable: boolean;
  /** Whether the account there also signs the user in. */
  readonly login: boolean;
  readonly apps: readonly UserConnection[];
}

/** The apps grouped by their kind, which is the provider they recognise users by. */
export function sectionsOf(
  apps: readonly UserConnection[],
  methods: readonly LoginMethod[],
): Section[] {
  const byProvider = new Map<string, UserConnection[]>();
  for (const app of apps)
    byProvider.set(app.provider, [...(byProvider.get(app.provider) ?? []), app]);
  return [...byProvider]
    .map(([provider, members]) => {
      const method = methods.find((one) => one.key === provider);
      return {
        provider,
        name: method?.name ?? provider,
        linkable: method?.link ?? false,
        login: method?.login ?? false,
        apps: [...members].sort((a, b) => a.name.localeCompare(b.name)),
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}
