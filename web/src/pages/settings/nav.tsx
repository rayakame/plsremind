import { Link } from "wouter";
import { useT, type Key } from "../../i18n/index.js";
import { routes } from "../../shell/router.js";
import { Icon } from "../../ui/icon.js";

type Group = "account" | "reminders" | "dashboard" | "developers" | "admin";

export interface SettingsPageInfo {
  readonly id: string;
  readonly group: Group;
  readonly label: Key;
  readonly admin?: true;
}

export const settingsPages: readonly SettingsPageInfo[] = [
  { id: "profile", group: "account", label: "settings.profile" },
  { id: "security", group: "account", label: "settings.security" },
  { id: "reminders", group: "reminders", label: "settings.reminders" },
  { id: "apps", group: "reminders", label: "settings.apps" },
  { id: "look", group: "dashboard", label: "settings.look" },
  { id: "keys", group: "dashboard", label: "settings.keys" },
  { id: "tokens", group: "developers", label: "settings.tokens" },
  { id: "admin/instance", group: "admin", label: "settings.instance", admin: true },
  { id: "admin/users", group: "admin", label: "settings.users", admin: true },
  { id: "admin/providers", group: "admin", label: "settings.providers", admin: true },
  { id: "admin/apps", group: "admin", label: "settings.adminApps", admin: true },
];

/** Addresses the settings had before they were grouped. */
export const legacySettings: Readonly<Record<string, string>> = {
  personal: "profile",
  connections: "apps",
  login: "security",
  admin: "admin/instance",
  "admin/invites": "admin/users",
  "admin/connections": "admin/apps",
};

const groups: readonly { id: Group; label: Key }[] = [
  { id: "account", label: "settings.groupAccount" },
  { id: "reminders", label: "settings.groupReminders" },
  { id: "dashboard", label: "settings.groupDashboard" },
  { id: "developers", label: "settings.groupDevelopers" },
  { id: "admin", label: "settings.groupAdmin" },
];

export function SettingsNav(props: {
  pages: readonly SettingsPageInfo[];
  current: string | undefined;
  warn: boolean;
}) {
  const t = useT();
  return (
    <nav className="snav" aria-label={t("nav.settings")}>
      {groups.map((group) => {
        const pages = props.pages.filter((page) => page.group === group.id);
        if (pages.length === 0) return null;
        return [
          <h2 key={group.id} className="label">
            {t(group.label)}
          </h2>,
          ...pages.map((page) => (
            <Link
              key={page.id}
              href={routes.settings(page.id)}
              className={page.id === props.current ? "nav on" : "nav"}
              aria-current={page.id === props.current ? "page" : undefined}
            >
              <span className="t">{t(page.label)}</span>
              {page.id === "apps" && props.warn && (
                <Icon name="warn" className="warn" label={t("settings.appsWarn")} />
              )}
            </Link>
          )),
        ];
      })}
    </nav>
  );
}
