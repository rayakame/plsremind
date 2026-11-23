import {
  formatDuration,
  parseDuration,
  type FollowUp,
  type Priority,
  type QuietHours,
  type ReminderPlan,
} from "plsremind";
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { usePlans, useProfile } from "../../hub/hooks.js";
import { useHub } from "../../hub/session.js";
import { resetPlan, setPlan, setQuiet } from "../../hub/write.js";
import { useT, type Key } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { durationWords, listWords } from "../../time/duration-words.js";
import { Button, Field, Input, Segmented, Toggle } from "../../ui/controls.js";
import { useDraft } from "../../ui/draft.js";
import { Icon } from "../../ui/icon.js";
import { Card } from "../../ui/layout.js";

const priorities: readonly Priority[] = ["high", "normal", "low"];
// The hub keeps at most this many times before a deadline.
const mostBefore = 10;

const sentences: Record<"due" | "before", Record<FollowUp, Key>> = {
  due: { never: "plan.dueNever", daily: "plan.dueDaily", twice_daily: "plan.dueTwice" },
  before: { never: "plan.beforeNever", daily: "plan.beforeDaily", twice_daily: "plan.beforeTwice" },
};

/** What a reminder plan does, in one sentence. */
export function planSentence(plan: ReminderPlan, t: ReturnType<typeof useT>): string {
  if (plan.before.length === 0) return t(sentences.due[plan.followUp]);
  const before = listWords([...plan.before].sort((a, b) => b - a).map(durationWords));
  return t(sentences.before[plan.followUp], { before });
}

function readTime(text: string): number | undefined {
  try {
    const ms = parseDuration(text.trim());
    // Only what can be written out again is a time the hub takes.
    formatDuration(ms);
    return ms;
  } catch {
    return undefined;
  }
}

function PlanCard({ plan }: { plan: ReminderPlan }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState("");
  const [error, setError] = useState<string>();
  const addButton = useRef<HTMLButtonElement>(null);
  // The button a removed chip or a closed field leaves focus to; without it focus falls to the page.
  const [refocus, setRefocus] = useState(false);
  useEffect(() => {
    if (refocus && !adding) {
      addButton.current?.focus();
      setRefocus(false);
    }
  }, [refocus, adding]);
  const title = t("personal.planOf", { priority: t(`priority.${plan.priority}`) });
  const save = (before: number[]) => void guard(setPlan(hub, plan.priority, { before }));
  const close = () => {
    setText("");
    setError(undefined);
    setAdding(false);
    setRefocus(true);
  };
  const escape = (event: KeyboardEvent) => {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    close();
  };

  const add = (event: FormEvent) => {
    event.preventDefault();
    const ms = readTime(text);
    if (ms === undefined) return setError(t("plan.timeError"));
    if (plan.before.includes(ms)) return setError(t("plan.timeTaken"));
    if (plan.before.length >= mostBefore) return setError(t("plan.timeLimit"));
    save([...plan.before, ms].sort((a, b) => b - a));
    close();
  };

  return (
    <section className="card" aria-label={title}>
      <div className="between">
        <span className="strong">{title}</span>
        {!plan.isDefault && (
          <Button variant="ghost" onClick={() => void guard(resetPlan(hub, plan.priority))}>
            {t("personal.planReset")}
          </Button>
        )}
      </div>
      <p className="text">{planSentence(plan, t)}</p>
      <div className="field">
        <span className="label">{t("plan.before")}</span>
        <div className="chips">
          <div role="list" className="chips" aria-label={t("plan.before")}>
            {plan.before.map((ms) => (
              <span role="listitem" className="chip" key={ms}>
                {durationWords(ms)}
                <button
                  type="button"
                  aria-label={t("plan.remove", { time: durationWords(ms) })}
                  onClick={() => {
                    save(plan.before.filter((one) => one !== ms));
                    setRefocus(true);
                  }}
                >
                  <Icon name="close" />
                </button>
              </span>
            ))}
          </div>
          {plan.before.length === 0 && <span className="dim">{t("plan.none")}</span>}
          {!adding && (
            <button
              ref={addButton}
              type="button"
              className="btn add"
              onClick={() => setAdding(true)}
            >
              {t("plan.add")}
            </button>
          )}
        </div>
        {adding && (
          <form className="line" onSubmit={add}>
            <Field label={t("plan.newTime")} error={error}>
              <Input
                autoFocus
                placeholder="2h"
                value={text}
                aria-invalid={error ? true : undefined}
                onChange={(event) => setText(event.target.value)}
                onKeyDown={escape}
              />
            </Field>
          </form>
        )}
      </div>
      <div className="between">
        <span>{t("personal.followUp")}</span>
        <Segmented<FollowUp>
          label={`${t("personal.followUp")}, ${title}`}
          value={plan.followUp}
          options={[
            { value: "never", label: t("followUp.never") },
            { value: "daily", label: t("followUp.daily") },
            { value: "twice_daily", label: t("followUp.twice_daily") },
          ]}
          onChange={(followUp) => void guard(setPlan(hub, plan.priority, { followUp }))}
        />
      </div>
    </section>
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
      <p className="lead">{t("plan.lead")}</p>
      <Card title={t("personal.quiet")}>
        <p className="text dim">
          {quiet ? t("plan.quietUntil", { end: quiet.end }) : t("personal.quietText")}
        </p>
        <Toggle
          label={t("personal.quiet")}
          checked={quiet !== null}
          onChange={(on) => changeQuiet(on ? {} : null)}
        />
        {quiet && <QuietTimes quiet={quiet} onChange={changeQuiet} />}
      </Card>

      <section className="stack" aria-label={t("personal.plans")}>
        <h2 className="h2">{t("personal.plans")}</h2>
        <p className="text dim">{t("plan.text")}</p>
        {priorities.map((priority) => {
          const plan = plans.find((one) => one.priority === priority);
          return plan ? <PlanCard key={priority} plan={plan} /> : null;
        })}
      </section>
    </>
  );
}
