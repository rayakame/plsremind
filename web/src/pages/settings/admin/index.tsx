import { Link } from "wouter";
import { useT, type Key } from "../../../i18n/index.js";
import { NotFound } from "../../not-found.js";
import { AdminConnections } from "./connections.js";
import { AdminInstance } from "./instance.js";
import { AdminInvites } from "./invites.js";
import { AdminProviders } from "./providers.js";
import { AdminUsers } from "./users.js";

const sections: readonly { id: string; label: Key }[] = [
  { id: "instance", label: "admin.instance" },
  { id: "users", label: "admin.users" },
  { id: "invites", label: "admin.invites" },
  { id: "providers", label: "admin.providers" },
  { id: "connections", label: "admin.connections" },
];

export function AdminSettings(props: { section: string | undefined }) {
  const t = useT();
  const section = props.section ?? "instance";
  if (!sections.some((one) => one.id === section)) return <NotFound />;
  return (
    <>
      <nav className="line" aria-label={t("settings.admin")}>
        {sections.map((one) => (
          <Link
            key={one.id}
            href={`/settings/admin/${one.id}`}
            className={one.id === section ? "btn on" : "btn ghost"}
            aria-current={one.id === section ? "page" : undefined}
          >
            {t(one.label)}
          </Link>
        ))}
      </nav>
      {section === "instance" && <AdminInstance />}
      {section === "users" && <AdminUsers />}
      {section === "invites" && <AdminInvites />}
      {section === "providers" && <AdminProviders />}
      {section === "connections" && <AdminConnections />}
    </>
  );
}
