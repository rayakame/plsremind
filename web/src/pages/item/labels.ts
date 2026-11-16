import { formatDuration, type ReminderKind } from "plsremind";
import { t } from "../../i18n/index.js";
import { formatMoment as moment } from "../../time/format.js";
import type { Now } from "../../model/groups.js";

export function reminderLabel(kind: ReminderKind, offset: number | null | undefined): string {
  if (kind === "before") {
    return t("reminder.before", { duration: offset ? formatDuration(offset) : "" });
  }
  return t(`reminder.${kind}`);
}

/** A moment as "tomorrow 19:00" or "Fri 08:00". */
export function formatMoment(instant: Date, now: Now): string {
  return moment(instant, now.zone, now.today);
}
