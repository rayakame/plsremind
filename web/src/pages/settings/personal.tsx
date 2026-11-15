import {
  formatDuration,
  parseDuration,
  type FollowUp,
  type Priority,
  type ReminderPlan,
} from "plsremind";
import { useState } from "react";
import { usePlans, useProfile } from "../../hub/hooks.js";
import { isPrefix, readPrefs, type Prefs } from "../../hub/prefs.js";
import { useHub } from "../../hub/session.js";
import { setPlan, setPrefs, updateProfile } from "../../hub/write.js";
import { languages, useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { isZone } from "../../time/zone.js";
import { Button, Field, Input, Segmented, Select, Toggle } from "../../ui/controls.js";
import { useDraft } from "../../ui/draft.js";
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
    return parts.map(parseDuration);
  } catch {
    return undefined;
  }
}

function PlanCard({ plan }: { plan: ReminderPlan }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const [error, setError] = useState(false);
  const before = useDraft(plan.before.map(formatDuration).join(", "), (next) => {
    const parsed = durations(next);
    setError(!parsed);
    if (parsed)
      void guard(setPlan(hub, plan.priority, { before: parsed, followUp: plan.followUp }));
  });
  return (
    <fieldset
      className="card"
      aria-label={t("personal.planOf", { priority: t(`priority.${plan.priority}`) })}
    >
      <legend className="label">
        {t("personal.planOf", { priority: t(`priority.${plan.priority}`) })}
      </legend>
      <Field label={t("item.overrideBefore")} error={error ? t("item.overrideError") : undefined}>
        <Input placeholder="1d, 2h" {...before} />
      </Field>
      <Field label={t("personal.followUp")}>
        <Select
          value={plan.followUp}
          onChange={(event) =>
            void guard(
              setPlan(hub, plan.priority, {
                before: plan.before,
                followUp: event.target.value as FollowUp,
              }),
            )
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
        <Button
          variant="ghost"
          onClick={() => void guard(hub.client.rest.resetReminderPlan(plan.priority))}
        >
          {t("personal.planReset")}
        </Button>
      )}
    </fieldset>
  );
}

export function PersonalSettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();
  const plans = usePlans();
  const prefs = readPrefs(profile.preferences);
  const [zoneError, setZoneError] = useState(false);
  const [prefixError, setPrefixError] = useState(false);

  const name = useDraft(
    profile.displayName,
    (next) => void guard(updateProfile(hub, { displayName: next })),
    { required: true, enter: true },
  );
  const zone = useDraft(
    profile.timezone,
    (next) => {
      const valid = isZone(next.trim()) && next.trim() !== "";
      setZoneError(!valid);
      if (valid) void guard(updateProfile(hub, { timezone: next.trim() }));
    },
    { enter: true },
  );
  const prefix = useDraft(
    prefs.prefix,
    (next) => {
      setPrefixError(!isPrefix(next));
      if (isPrefix(next)) void guard(setPrefs(hub, { prefix: next }));
    },
    { enter: true },
  );
  const quiet = profile.quietHours;
  const setQuiet = (next: { start: string; end: string } | null) =>
    void guard(updateProfile(hub, { quietHours: next }));

  return (
    <>
      <Card title={t("personal.profile")}>
        <Field label={t("personal.displayName")}>
          <Input {...name} />
        </Field>
        <Field
          label={t("personal.timezone")}
          error={zoneError ? t("personal.timezoneError") : undefined}
        >
          <Input list="zones" {...zone} />
        </Field>
        <datalist id="zones">
          {zones.map((one) => (
            <option key={one} value={one} />
          ))}
        </datalist>
        <Field label={t("personal.language")}>
          <Select
            value={profile.locale?.toLowerCase().split("-")[0] ?? ""}
            onChange={(event) =>
              void guard(updateProfile(hub, { locale: event.target.value || null }))
            }
          >
            <option value="">{t("personal.languageBrowser")}</option>
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
          onChange={(on) => setQuiet(on ? { start: "22:00", end: "07:00" } : null)}
        />
        {quiet && (
          <div className="line">
            <Field label={t("personal.quietFrom")}>
              <Input
                type="time"
                value={quiet.start}
                onChange={(event) => {
                  if (event.target.value) setQuiet({ ...quiet, start: event.target.value });
                }}
              />
            </Field>
            <Field label={t("personal.quietTo")}>
              <Input
                type="time"
                value={quiet.end}
                onChange={(event) => {
                  if (event.target.value) setQuiet({ ...quiet, end: event.target.value });
                }}
              />
            </Field>
          </div>
        )}
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
          error={prefixError ? t("personal.prefixError") : undefined}
        >
          <Input maxLength={2} {...prefix} />
        </Field>
        <KeysEditor />
      </Card>
    </>
  );
}
