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
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { resetPlan, setPlan, setQuiet } from "../../hub/write.js";
import { useNotices } from "../../shell/notices.js";
import { Button, Field, Input, Select, Toggle } from "../../ui/controls.js";
import { useCheckedDraft, useDraft } from "../../ui/draft.js";
import { Card } from "../../ui/layout.js";

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

export function ReminderSettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();
  const plans = usePlans();
  const quiet = profile.quietHours;
  const changeQuiet = (part: Partial<QuietHours> | null) => void guard(setQuiet(hub, part));

  return (
    <>
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
    </>
  );
}
