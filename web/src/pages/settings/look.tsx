import { useProfile } from "../../hub/hooks.js";
import { readPrefs, type Prefs } from "../../hub/prefs.js";
import { useHub } from "../../hub/session.js";
import { setPrefs } from "../../hub/write.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Segmented } from "../../ui/controls.js";
import { Card } from "../../ui/layout.js";

export function LookSettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const prefs = readPrefs(useProfile().preferences);

  return (
    <Card>
      <div className="between">
        <span>{t("personal.theme")}</span>
        <Segmented<Prefs["theme"]>
          label={t("personal.theme")}
          value={prefs.theme}
          options={[
            { value: "system", label: t("theme.system") },
            { value: "light", label: t("theme.light") },
            { value: "dark", label: t("theme.dark") },
          ]}
          onChange={(theme) => void guard(setPrefs(hub, { theme }))}
        />
      </div>
      <div className="between">
        <span>{t("personal.weekStart")}</span>
        <Segmented<Prefs["weekStart"]>
          label={t("personal.weekStart")}
          value={prefs.weekStart}
          options={[
            { value: "monday", label: t("personal.monday") },
            { value: "sunday", label: t("personal.sunday") },
          ]}
          onChange={(weekStart) => void guard(setPrefs(hub, { weekStart }))}
        />
      </div>
    </Card>
  );
}
