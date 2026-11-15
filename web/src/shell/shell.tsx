import { useEffect, useState, type ReactNode } from "react";
import { useLocation } from "wouter";
import { useT } from "../i18n/index.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { ConnectionLine, ZoneLine } from "./lines.js";
import { Sidebar } from "./sidebar.js";

export function Shell(props: { panel?: ReactNode; children: ReactNode }) {
  const t = useT();
  const [drawer, setDrawer] = useState(false);
  const [path] = useLocation();
  useEffect(() => setDrawer(false), [path]);

  return (
    <div className={props.panel ? "app with-panel" : "app"}>
      <Sidebar open={drawer} onNavigate={() => setDrawer(false)} />
      <main className="main">
        <div className="barrow">
          <Button
            variant="icon"
            className="only-narrow"
            aria-label={t("nav.menu")}
            aria-expanded={drawer}
            onClick={() => setDrawer(!drawer)}
          >
            <Icon name="menu" />
          </Button>
        </div>
        <ConnectionLine />
        <ZoneLine />
        {props.children}
      </main>
      {props.panel}
    </div>
  );
}
