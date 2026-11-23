import { Link, Redirect } from "wouter";
import { useProfile } from "../../hub/hooks.js";
import type { UserConnection } from "plsremind";
import { useLoaded, type Loaded } from "../../hub/loaded.js";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { routes } from "../../shell/router.js";
import { Icon } from "../../ui/icon.js";
import { useNarrow } from "../../ui/layout-mode.js";
import { NotFound } from "../not-found.js";
import { AdminApps } from "./admin/apps.js";
import { AdminInstance } from "./admin/instance.js";
import { AdminInvites } from "./admin/invites.js";
import { AdminProviders } from "./admin/providers.js";
import { AdminUsers } from "./admin/users.js";
import { needsAttention } from "./apps-model.js";
import { AppsSettings } from "./apps.js";
import { KeySettings } from "./keys.js";
import { LookSettings } from "./look.js";
import { legacySettings, settingsPages, SettingsNav } from "./nav.js";
import { ProfileSettings } from "./profile.js";
import { ReminderSettings } from "./reminders.js";
import { SecuritySettings } from "./security.js";
import { TokenSettings } from "./tokens.js";

function Content(props: {
  page: string;
  rest: string | undefined;
  apps: Loaded<UserConnection[]>;
}) {
  const { page, apps } = props;
  switch (page) {
    case "security":
      return <SecuritySettings />;
    case "reminders":
      return <ReminderSettings />;
    case "look":
      return <LookSettings />;
    case "keys":
      return <KeySettings />;
    case "apps":
      return <AppsSettings apps={apps} />;
    case "tokens":
      return <TokenSettings />;
    case "admin/instance":
      return <AdminInstance />;
    case "admin/users":
      return (
        <>
          <AdminUsers />
          <AdminInvites />
        </>
      );
    case "admin/providers":
      return <AdminProviders />;
    case "admin/apps":
      return <AdminApps rest={props.rest} />;
    default:
      return <ProfileSettings />;
  }
}

export default function SettingsPage(props: { path: string | undefined }) {
  const t = useT();
  const { client } = useHub();
  const profile = useProfile();
  const narrow = useNarrow();
  const apps = useLoaded((signal) => client.rest.listUserConnections({ signal }), client);
  const warn = needsAttention(apps.data ?? []);
  const allowed = settingsPages.filter((page) => !page.admin || profile.role === "admin");

  const id = props.path?.replace(/\/+$/, "");
  if (!id) {
    if (!narrow) return <Redirect to={routes.settings("profile")} replace />;
    return (
      <div className="scroll col">
        <h1 className="h1">{t("nav.settings")}</h1>
        <SettingsNav pages={allowed} current={undefined} warn={warn} />
      </div>
    );
  }
  const moved = legacySettings[id];
  if (moved !== undefined) return <Redirect to={routes.settings(moved)} replace />;
  const page = allowed.find((one) => id === one.id || id.startsWith(`${one.id}/`));
  const rest = page && id !== page.id ? id.slice(page.id.length + 1) : undefined;
  if (!page || (rest !== undefined && page.id !== "admin/apps")) return <NotFound />;

  return (
    <div className="scroll col">
      <div className="settings">
        {!narrow && <SettingsNav pages={allowed} current={page.id} warn={warn} />}
        <div className="page">
          {narrow && (
            <Link href={routes.settings()} className="btn ghost back">
              <Icon name="left" />
              {t("settings.all")}
            </Link>
          )}
          <h1 className="h1">{t(page.label)}</h1>
          <Content page={page.id} rest={rest} apps={apps} />
        </div>
      </div>
    </div>
  );
}
