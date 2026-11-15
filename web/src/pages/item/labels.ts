import { formatDuration, type ReminderKind } from "plsremind";
import { t } from "../../i18n/index.js";
import { formatMoment } from "../../time/format.js";

export function reminderLabel(kind: ReminderKind, offset: number | null | undefined): string {
  if (kind === "before") {
    return t("reminder.before", { duration: offset ? formatDuration(offset) : "" });
  }
  return t(`reminder.${kind}`);
}

export { formatMoment };
