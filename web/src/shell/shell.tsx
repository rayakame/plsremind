import { useEffect, useState, type ReactNode } from "react";
import { useLocation } from "wouter";
import { useT } from "../i18n/index.js";
import { Bar } from "../bar/bar.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { ConnectionLine, ZoneLine } from "./lines.js";
import { useSelection } from "./selection.js";
import { Sidebar } from "./sidebar.js";

export function Shell(props: { panel: ReactNode; children: ReactNode }) {
  const { selected } = useSelection();
  const t = useT();
  const [drawer, setDrawer] = useState(false);
  const [path] = useLocation();
  useEffect(() => setDrawer(false), [path]);
  useEffect(() => {
    if (!drawer) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setDrawer(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawer]);

  return (
    <div className={selected !== undefined ? "app with-panel" : "app"}>
      <Sidebar open={drawer} onNavigate={() => setDrawer(false)} />
      {drawer && <div className="backdrop only-narrow" onClick={() => setDrawer(false)} />}
      <main className="main fixed">
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
          <Bar />
        </div>
        <ConnectionLine />
        <ZoneLine />
        {props.children}
      </main>
      {props.panel}
    </div>
  );
}
