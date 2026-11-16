import { useEffect, useRef, useState } from "react";
import { useRoute } from "wouter";
import { useBar } from "../bar/context.js";
import { useItem, useProfile } from "../hub/hooks.js";
import { readPrefs, type Prefs } from "../hub/prefs.js";
import { useHub } from "../hub/session.js";
import { setDone, setPrefs } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { useNotices } from "../shell/notices.js";
import { useLeaveItem, useOpenItem, useSelection } from "../shell/selection.js";
import { Dialog } from "../ui/dialog.js";
import { Kbd } from "../ui/layout.js";
import { actions, bindings, keysOf, type ActionId } from "./actions.js";

const nextTheme: Record<Prefs["theme"], Prefs["theme"]> = {
  system: "light",
  light: "dark",
  dark: "system",
};

const notTyped = new Set(["checkbox", "radio", "button", "submit", "reset"]);

function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target instanceof HTMLInputElement) return !notTyped.has(target.type);
  return (
    target.isContentEditable ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement
  );
}

/** Single keys for everything frequent. They act while no field has the focus. */
export function Keys() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const bar = useBar();
  const profile = useProfile();
  const prefs = readPrefs(profile.preferences);
  const { selected, select, order, listed, pageItem } = useSelection();
  const [, onItem] = useRoute("/items/:id");
  const item = useItem(onItem?.id ?? selected) ?? (onItem ? pageItem : undefined);
  const open = useOpenItem();
  const leave = useLeaveItem();
  const [help, setHelp] = useState(false);

  const run: Record<ActionId, () => void> = {
    down: () => move(1),
    up: () => move(-1),
    open: () => {
      if (selected !== undefined) open(selected);
    },
    complete: () => {
      if (item) void guard(setDone(hub, item, item.status !== "done"));
    },
    new: () => bar.focus(`${bar.prefix}new `),
    find: () => bar.focus(""),
    remind: () => bar.focus(`${bar.prefix}remind `),
    due: () => bar.focus(`${bar.prefix}due `),
    move: () => bar.focus(`${bar.prefix}move #`),
    goto: () => bar.focus(`${bar.prefix}go `),
    theme: () => void guard(setPrefs(hub, { theme: nextTheme[prefs.theme] })),
    help: () => setHelp(true),
    back: () => {
      if (selected !== undefined) select(undefined);
      else if (bar.defaultDue !== null) bar.setDefaultDue(null);
      else if (onItem) leave();
    },
  };
  function move(by: number) {
    if (!listed || order.length === 0) return;
    const at = selected === undefined ? -1 : order.indexOf(selected);
    const next = at < 0 ? (by > 0 ? 0 : order.length - 1) : at + by;
    select(order[Math.min(order.length - 1, Math.max(0, next))]);
    // A link or button that still has the focus would take the Enter meant for the item.
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  }

  const now = { run, keys: prefs.keys, prefix: bar.prefix, focus: bar.focus, listed };
  const latest = useRef(now);
  latest.current = now;

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.altKey || event.metaKey || event.defaultPrevented) return;
      if (typing(event.target) || document.querySelector('[role="dialog"]')) return;
      const now = latest.current;
      let action = bindings(now.keys).get(event.key);
      if (now.listed && event.key === "ArrowDown") action = "down";
      if (now.listed && event.key === "ArrowUp") action = "up";
      if ((action === "down" || action === "up") && !now.listed) return;
      const onControl =
        event.target instanceof HTMLButtonElement || event.target instanceof HTMLAnchorElement;
      if (action === "open" && onControl) return;
      if (action) {
        event.preventDefault();
        now.run[action]();
      } else if (event.key === now.prefix) {
        event.preventDefault();
        now.focus(now.prefix);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  if (!help) return null;
  const keys = keysOf(prefs.keys);
  return (
    <Dialog title={t("keys.title")} onClose={() => setHelp(false)}>
      <div role="list" className="stack">
        {actions.map((action) => (
          <div role="listitem" className="kv" key={action}>
            <span>{t(`keys.${action}`)}</span>
            <Kbd>{keys[action]}</Kbd>
          </div>
        ))}
      </div>
    </Dialog>
  );
}
