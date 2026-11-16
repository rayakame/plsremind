import {
  formatDuration,
  parseDuration,
  type FollowUp,
  type Priority,
  type QuietHours,
  type ReminderPlan,
} from "plsremind";
import { useState } from "react";
import { usePlans, useProfile } from "../../hub/hooks.js";
import { isPrefix, readPrefs, type Prefs } from "../../hub/prefs.js";
import { useHub } from "../../hub/session.js";
import { resetPlan, setPlan, setPrefs, setQuiet, updateProfile } from "../../hub/write.js";
import { languages, useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { isZone } from "../../time/zone.js";
import { Button, Field, Input, Segmented, Select, Toggle } from "../../ui/controls.js";
import { keysOf } from "../../keys/actions.js";
import { useCheckedDraft, useDraft } from "../../ui/draft.js";
import { Card } from "../../ui/layout.js";
import { KeysEditor } from "./keys-editor.js";

const languageNames: Record<string, string> = { en: "English", de: "Deutsch" };
const zones: readonly string[] =
  typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
const priorities: readonly Priority[] = ["high", "normal", "low"];
const followUps: readonly FollowUp[] = ["never", "daily", "twice_daily"];

function durations(text: string): number[] | undefined {
  const parts = text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
  try {
    const all = parts.map(parseDuration);
    // Only what can be written out again is a duration the page can show.
    all.forEach(formatDuration);
    return all;
  } catch {
    return undefined;
  }
}

function PlanCard({ plan }: { plan: ReminderPlan }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const before = useCheckedDraft(
    plan.before.map(formatDuration).join(", "),
    durations,
    (parsed) => void guard(setPlan(hub, plan.priority, { before: parsed })),
  );
  return (
    <fieldset
      className="card"
      aria-label={t("personal.planOf", { priority: t(`priority.${plan.priority}`) })}
    >
      <legend className="label">
        {t("personal.planOf", { priority: t(`priority.${plan.priority}`) })}
      </legend>
      <Field
        label={t("item.overrideBefore")}
        error={before.invalid ? t("item.overrideError") : undefined}
      >
        <Input placeholder="1d, 2h" {...before.field} />
      </Field>
      <Field label={t("personal.followUp")}>
        <Select
          value={plan.followUp}
          onChange={(event) =>
            void guard(setPlan(hub, plan.priority, { followUp: event.target.value as FollowUp }))
          }
        >
          {followUps.map((one) => (
            <option key={one} value={one}>
              {t(`followUp.${one}`)}
            </option>
          ))}
        </Select>
      </Field>
      {!plan.isDefault && (
        <Button variant="ghost" onClick={() => void guard(resetPlan(hub, plan.priority))}>
          {t("personal.planReset")}
        </Button>
      )}
    </fieldset>
  );
}

function QuietTimes(props: { quiet: QuietHours; onChange: (part: Partial<QuietHours>) => void }) {
  const t = useT();
  const { quiet, onChange } = props;
  const start = useDraft(quiet.start, (next) => onChange({ start: next }), {
    required: true,
    enter: true,
  });
  const end = useDraft(quiet.end, (next) => onChange({ end: next }), {
    required: true,
    enter: true,
  });
  return (
    <div className="line">
      <Field label={t("personal.quietFrom")}>
        <Input type="time" {...start} />
      </Field>
      <Field label={t("personal.quietTo")}>
        <Input type="time" {...end} />
      </Field>
    </div>
  );
}

export function PersonalSettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();
  const plans = usePlans();
  const prefs = readPrefs(profile.preferences);

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
  const taken = new Set(Object.values(keysOf(prefs.keys)));
  const prefix = useCheckedDraft(
    prefs.prefix,
    (next) => (isPrefix(next) && !taken.has(next) ? next : undefined),
    (next) => void guard(setPrefs(hub, { prefix: next })),
    { enter: true },
  );
  const base = profile.locale?.toLowerCase().split("-")[0] ?? "";
  const language = base in languages ? base : (profile.locale ?? "");
  const quiet = profile.quietHours;
  const changeQuiet = (part: Partial<QuietHours> | null) => void guard(setQuiet(hub, part));

  return (
    <>
      <Card title={t("personal.profile")}>
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
        <Field label={t("personal.language")}>
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

      <Card title={t("personal.look")}>
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

      <Card title={t("personal.quiet")}>
        <p className="text dim">{t("personal.quietText")}</p>
        <Toggle
          label={t("personal.quiet")}
          checked={quiet !== null}
          onChange={(on) => changeQuiet(on ? {} : null)}
        />
        {quiet && <QuietTimes quiet={quiet} onChange={changeQuiet} />}
      </Card>

      <section className="stack" aria-label={t("personal.plans")}>
        <h2 className="h2">{t("personal.plans")}</h2>
        {priorities.map((priority) => {
          const plan = plans.find((one) => one.priority === priority);
          return plan ? <PlanCard key={priority} plan={plan} /> : null;
        })}
      </section>

      <Card title={t("personal.commands")}>
        <Field
          label={t("personal.prefix")}
          error={prefix.invalid ? t("personal.prefixError") : undefined}
        >
          <Input maxLength={2} {...prefix.field} />
        </Field>
        <KeysEditor />
      </Card>
    </>
  );
}
