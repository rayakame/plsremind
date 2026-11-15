import { Link } from "wouter";
import { useProfile } from "../../hub/hooks.js";
import { useT, type Key } from "../../i18n/index.js";
import { routes } from "../../shell/router.js";
import { NotFound } from "../not-found.js";
import { AdminSettings } from "./admin/index.js";
import { ConnectionSettings } from "./connections.js";
import { LoginSettings } from "./login.js";
import { PersonalSettings } from "./personal.js";
import { TokenSettings } from "./tokens.js";

const areas: readonly { id: string; label: Key; admin?: true }[] = [
  { id: "personal", label: "settings.personal" },
  { id: "connections", label: "settings.connections" },
  { id: "tokens", label: "settings.tokens" },
  { id: "login", label: "settings.login" },
  { id: "admin", label: "settings.admin", admin: true },
];

export default function SettingsPage(props: {
  area: string | undefined;
  section: string | undefined;
}) {
  const t = useT();
  const profile = useProfile();
  const area = props.area ?? "personal";
  const allowed = areas.filter((one) => !one.admin || profile.role === "admin");
  if (!allowed.some((one) => one.id === area)) return <NotFound />;

  return (
    <div className="scroll col">
      <h1 className="h1">{t("nav.settings")}</h1>
      <nav className="line" aria-label={t("nav.settings")}>
        {allowed.map((one) => (
          <Link
            key={one.id}
            href={routes.settings(one.id)}
            className={one.id === area ? "btn on" : "btn ghost"}
            aria-current={one.id === area ? "page" : undefined}
          >
            {t(one.label)}
          </Link>
        ))}
      </nav>
      <div className="page">
        {area === "personal" && <PersonalSettings />}
        {area === "connections" && <ConnectionSettings />}
        {area === "tokens" && <TokenSettings />}
        {area === "login" && <LoginSettings />}
        {area === "admin" && <AdminSettings section={props.section} />}
      </div>
    </div>
  );
}
