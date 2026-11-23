import { Link, Redirect } from "wouter";
import { useProfile } from "../../hub/hooks.js";
import { useLoaded } from "../../hub/loaded.js";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { routes } from "../../shell/router.js";
import { Icon } from "../../ui/icon.js";
import { useNarrow } from "../../ui/layout-mode.js";
import { NotFound } from "../not-found.js";
import { AdminConnections } from "./admin/connections.js";
import { AdminInstance } from "./admin/instance.js";
import { AdminInvites } from "./admin/invites.js";
import { AdminProviders } from "./admin/providers.js";
import { AdminUsers } from "./admin/users.js";
import { needsAttention } from "./apps-model.js";
import { ConnectionSettings } from "./connections.js";
import { LoginSettings } from "./login.js";
import { legacySettings, settingsPages, SettingsNav } from "./nav.js";
import { PersonalSettings } from "./personal.js";
import { TokenSettings } from "./tokens.js";

function Content({ page }: { page: string }) {
  switch (page) {
    case "security":
      return <LoginSettings />;
    case "apps":
      return <ConnectionSettings />;
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
      return <AdminConnections />;
    default:
      return <PersonalSettings />;
  }
}

export default function SettingsPage(props: {
  area: string | undefined;
  section: string | undefined;
}) {
  const t = useT();
  const { client } = useHub();
  const profile = useProfile();
  const narrow = useNarrow();
  const apps = useLoaded((signal) => client.rest.listUserConnections({ signal }), client);
  const warn = needsAttention(apps.data ?? []);
  const allowed = settingsPages.filter((page) => !page.admin || profile.role === "admin");

  const id = props.section ? `${props.area}/${props.section}` : props.area;
  if (id === undefined) {
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
  const page = allowed.find((one) => one.id === id);
  if (!page) return <NotFound />;

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
          <Content page={page.id} />
        </div>
      </div>
    </div>
  );
}
