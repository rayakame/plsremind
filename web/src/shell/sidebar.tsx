import { useState } from "react";
import { Link, useLocation } from "wouter";
import { createList } from "../hub/write.js";
import { Icon } from "../ui/icon.js";
import { NameDialog } from "../ui/name-dialog.js";
import { useNotices } from "./notices.js";
import { useItems, useLists, useProfile } from "../hub/hooks.js";
import { useHub, useSessionStore } from "../hub/session.js";
import { useT } from "../i18n/index.js";
import { openCounts } from "../model/counts.js";
import { Button } from "../ui/controls.js";
import { NavItem } from "../ui/layout.js";
import { routes } from "./router.js";

export function Sidebar(props: { open: boolean; onNavigate: () => void }) {
  const t = useT();
  const hub = useHub();
  const { info } = hub;
  const session = useSessionStore();
  const profile = useProfile();
  const lists = useLists();
  const items = useItems();
  const counts = openCounts(items);
  const [path, navigate] = useLocation();
  const [creating, setCreating] = useState(false);
  const { guard } = useNotices();

  const link = (href: string, label: string, count?: number, active = path === href) => (
    <Link href={href} asChild>
      <NavItem active={active} count={count} onClick={props.onNavigate}>
        {label}
      </NavItem>
    </Link>
  );

  return (
    <nav className={props.open ? "side open" : "side"} aria-label={t("nav.label")}>
      <div className="brand">{info.name}</div>
      {link(routes.items, t("nav.all"), items.length)}
      {link(routes.calendar, t("nav.calendar"))}
      <h2 className="label">{t("nav.lists")}</h2>
      {lists.map((list) => (
        <div key={list.id}>{link(routes.list(list.id), list.name, counts.get(list.id) ?? 0)}</div>
      ))}
      <Button variant="ghost" onClick={() => setCreating(true)}>
        <Icon name="plus" />
        {t("list.new")}
      </Button>
      <div className="grow" />
      {link(routes.done, t("nav.done"))}
      {link(routes.settings(), t("nav.settings"), undefined, path.startsWith("/settings"))}
      <div className="between">
        <span className="label">{profile.displayName}</span>
        <Button variant="ghost" onClick={() => void guard(session.signOut())}>
          {t("nav.signOut")}
        </Button>
      </div>
      {creating && (
        <NameDialog
          title={t("list.new")}
          submit={t("common.create")}
          onSubmit={(name) =>
            void guard(createList(hub, name)).then((id) => {
              if (id !== undefined) navigate(routes.list(id));
            })
          }
          onClose={() => setCreating(false)}
        />
      )}
    </nav>
  );
}
