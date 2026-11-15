import { useT } from "../i18n/index.js";
import { parseClock, showClock } from "../time/clock.js";
import { Field, Input } from "./controls.js";
import { useCheckedDraft } from "./draft.js";

/**
 * A time of day as text on the user's clock. It reads both notations, whatever it shows,
 * and saves "HH:MM" when it is left. The browser's own time field ignores the user's choice.
 */
export function TimeField(props: {
  label: string;
  value: string;
  hour12: boolean;
  required?: boolean;
  readOnly?: boolean;
  onSave: (time: string) => void;
}) {
  const t = useT();
  const draft = useCheckedDraft(
    showClock(props.value, props.hour12),
    (text) => {
      const time = parseClock(text);
      return time === "" && props.required ? undefined : time;
    },
    props.onSave,
    { enter: true },
  );
  return (
    <Field label={props.label} error={draft.invalid ? t("item.timeError") : undefined}>
      <Input
        inputMode="text"
        autoComplete="off"
        placeholder={showClock("18:00", props.hour12)}
        readOnly={props.readOnly}
        {...draft.field}
      />
    </Field>
  );
}
