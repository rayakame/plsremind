import { useState, type KeyboardEvent } from "react";
import { useProfile } from "../../hub/hooks.js";
import { readPrefs } from "../../hub/prefs.js";
import { useHub } from "../../hub/session.js";
import { setPrefs } from "../../hub/write.js";
import { useT } from "../../i18n/index.js";
import { actions, keysOf, rebind, type ActionId } from "../../keys/actions.js";
import { useNotices } from "../../shell/notices.js";
import { Button } from "../../ui/controls.js";
import { Kbd, Notice } from "../../ui/layout.js";

const ignored = new Set(["Shift", "Control", "Alt", "Meta", "Tab", "Escape", "CapsLock"]);

/** Lets every single-key shortcut be moved to another key. */
export function KeysEditor() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();
  const { keys: overrides, prefix } = readPrefs(profile.preferences);
  const [reserved, setReserved] = useState<string>();
  const [changing, setChanging] = useState<ActionId>();
  const [taken, setTaken] = useState<{ key: string; action: ActionId }>();
  const keys = keysOf(overrides);

  const onKeyDown = (event: KeyboardEvent, action: ActionId) => {
    if (changing !== action) return;
    if (event.key === "Escape") {
      setChanging(undefined);
      setTaken(undefined);
      return;
    }
    if (ignored.has(event.key) || event.ctrlKey || event.altKey || event.metaKey) return;
    event.preventDefault();
    event.stopPropagation();
    const result = rebind(overrides, action, event.key, prefix);
    if (!result.ok) {
      setTaken(result.conflict ? { key: event.key, action: result.conflict } : undefined);
      setReserved(event.key === prefix ? event.key : undefined);
      return;
    }
    setChanging(undefined);
    setTaken(undefined);
    setReserved(undefined);
    void guard(setPrefs(hub, { keys: result.overrides }));
  };

  return (
    <fieldset className="stack" aria-label={t("keys.title")}>
      <legend className="label">{t("keys.title")}</legend>
      {taken && (
        <Notice tone="danger" flat>
          {t("personal.keyTaken", { key: taken.key, action: t(`keys.${taken.action}`) })}
        </Notice>
      )}
      {reserved !== undefined && (
        <Notice tone="danger" flat>
          {t("personal.keyReserved", { key: reserved })}
        </Notice>
      )}
      {actions.map((action) => (
        <div className="between" key={action}>
          <span>{t(`keys.${action}`)}</span>
          <Button
            aria-label={t("personal.keyChange", { action: t(`keys.${action}`) })}
            aria-pressed={changing === action}
            onClick={() => {
              setChanging(changing === action ? undefined : action);
              setTaken(undefined);
            }}
            onKeyDown={(event) => onKeyDown(event, action)}
            onBlur={() => {
              if (changing === action) setChanging(undefined);
            }}
          >
            {changing === action ? t("personal.keyPress") : <Kbd>{keys[action]}</Kbd>}
          </Button>
        </div>
      ))}
      <Button
        variant="ghost"
        disabled={Object.keys(overrides).length === 0}
        onClick={() => void guard(setPrefs(hub, { keys: {} }))}
      >
        {t("personal.keysReset")}
      </Button>
    </fieldset>
  );
}
