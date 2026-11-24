import { useProfile } from "../../hub/hooks.js";
import { useHub } from "../../hub/session.js";
import { updateProfile } from "../../hub/write.js";
import { languages, useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { isZone } from "../../time/zone.js";
import { Field, Input, Select } from "../../ui/controls.js";
import { useCheckedDraft, useDraft } from "../../ui/draft.js";
import { Card } from "../../ui/layout.js";

const languageNames: Record<string, string> = { en: "English", de: "Deutsch" };
const zones: readonly string[] =
  typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];

export function ProfileSettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();

  const name = useDraft(
    profile.displayName,
    (next) => void guard(updateProfile(hub, { displayName: next })),
    { required: true, enter: true },
  );
  const zone = useCheckedDraft(
    profile.timezone,
    (next) => (next.trim() !== "" && isZone(next.trim()) ? next.trim() : undefined),
    (timezone) => void guard(updateProfile(hub, { timezone })),
    { enter: true },
  );
  const base = profile.locale?.toLowerCase().split("-")[0] ?? "";
  const language = base in languages ? base : (profile.locale ?? "");

  return (
    <Card>
      <Field label={t("personal.displayName")}>
        <Input {...name} />
      </Field>
      <Field
        label={t("personal.timezone")}
        error={zone.invalid ? t("personal.timezoneError") : undefined}
      >
        <Input list="zones" {...zone.field} />
      </Field>
      <datalist id="zones">
        {zones.map((one) => (
          <option key={one} value={one} />
        ))}
      </datalist>
      <Field
        label={t("personal.language")}
        hint={language === "" ? t("personal.languageBrowserHint") : undefined}
      >
        <Select
          value={language}
          onChange={(event) =>
            void guard(updateProfile(hub, { locale: event.target.value || null }))
          }
        >
          <option value="">{t("personal.languageBrowser")}</option>
          {language !== "" && !(language in languages) && (
            <option value={language}>{language}</option>
          )}
          {Object.keys(languages).map((tag) => (
            <option key={tag} value={tag}>
              {languageNames[tag] ?? tag}
            </option>
          ))}
        </Select>
      </Field>
    </Card>
  );
}
