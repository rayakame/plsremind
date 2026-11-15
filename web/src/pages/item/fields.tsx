import { formatDuration, parseDuration, type Item, type Priority, type Subitem } from "plsremind";
import { useState, type FormEvent } from "react";
import { useLists } from "../../hub/hooks.js";
import { useNow } from "../../hub/now.js";
import { useSchedule } from "../../hub/schedule.js";
import { useHub } from "../../hub/session.js";
import {
  addReminder,
  addSubitem,
  moveSubitem,
  patchItem,
  partKey,
  patchSubitem,
  removeReminder,
  removeSubitem,
} from "../../hub/write.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { parseWhen } from "../../time/when.js";
import { dayIn, instantAt, timeIn } from "../../time/zone.js";
import { Button, Field, Input, Select, TextArea } from "../../ui/controls.js";
import { useCheckedDraft, useDraft } from "../../ui/draft.js";
import { Icon } from "../../ui/icon.js";
import { Card, Row, Rows } from "../../ui/layout.js";
import { formatMoment, reminderLabel } from "./labels.js";

interface Props {
  item: Item;
  readOnly?: boolean;
  /** For the narrow detail column: leaves out what needs room. */
  compact?: boolean;
}

/** Title, note and subitems: what an item says. */
export function ItemText({ item, readOnly = false, compact = false }: Props) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const title = useDraft(item.title, (next) => void guard(patchItem(hub, item, { title: next })), {
    required: true,
    enter: true,
  });
  const note = useDraft(item.note, (next) => void guard(patchItem(hub, item, { note: next })));
  const [adding, setAdding] = useState("");

  const add = (event: FormEvent) => {
    event.preventDefault();
    const text = adding.trim();
    if (text === "") return;
    setAdding("");
    void guard(addSubitem(hub, item, text));
  };

  return (
    <div className="col">
      <Field label={t("item.title")}>
        <Input look="title" readOnly={readOnly} {...title} />
      </Field>
      <Field label={t("item.note")}>
        <TextArea readOnly={readOnly} rows={4} {...note} />
      </Field>
      <section className="stack" aria-label={t("item.subitems")}>
        <h3 className="label">{t("item.subitems")}</h3>
        {item.subitems.length > 0 && (
          <Rows>
            {item.subitems.map((subitem, index) => (
              <SubitemRow
                key={partKey(hub, subitem.id)}
                item={item}
                subitem={subitem}
                first={index === 0}
                last={index === item.subitems.length - 1}
                readOnly={readOnly}
                movable={!compact}
              />
            ))}
          </Rows>
        )}
        {!readOnly && (
          <form onSubmit={add}>
            <Input
              aria-label={t("item.addSubitem")}
              placeholder={t("item.addSubitem")}
              value={adding}
              onChange={(event) => setAdding(event.target.value)}
            />
          </form>
        )}
      </section>
    </div>
  );
}

function SubitemRow(props: {
  item: Item;
  subitem: Subitem;
  first: boolean;
  last: boolean;
  readOnly: boolean;
  movable: boolean;
}) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const { item, subitem } = props;
  const title = useDraft(
    subitem.title,
    (next) => void guard(patchSubitem(hub, item, subitem, { title: next })),
    { required: true, enter: true },
  );
  const named = { title: subitem.title };
  return (
    <Row done={subitem.done}>
      <input
        type="checkbox"
        className="check"
        checked={subitem.done}
        disabled={props.readOnly}
        aria-label={subitem.title}
        onChange={() => void guard(patchSubitem(hub, item, subitem, { done: !subitem.done }))}
      />
      <Input
        look="plain"
        className="fill"
        readOnly={props.readOnly}
        aria-label={t("item.subitemTitle", named)}
        {...title}
      />
      {!props.readOnly && props.movable && (
        <>
          <Button
            variant="icon"
            disabled={props.first}
            aria-label={t("item.moveUpNamed", named)}
            onClick={() => void guard(moveSubitem(hub, item, subitem, -1))}
          >
            <Icon name="up" />
          </Button>
          <Button
            variant="icon"
            disabled={props.last}
            aria-label={t("item.moveDownNamed", named)}
            onClick={() => void guard(moveSubitem(hub, item, subitem, 1))}
          >
            <Icon name="down" />
          </Button>
        </>
      )}
      {!props.readOnly && (
        <Button
          variant="icon"
          aria-label={t("item.removeNamed", named)}
          onClick={() => void guard(removeSubitem(hub, item, subitem))}
        >
          <Icon name="close" />
        </Button>
      )}
    </Row>
  );
}

const priorities: readonly Priority[] = ["high", "normal", "low"];

function durations(text: string): number[] | undefined {
  const parts = text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
  if (parts.length === 0) return undefined;
  try {
    const all = parts.map(parseDuration);
    return all.every((one) => Number.isFinite(one) && one > 0) ? all : undefined;
  } catch {
    return undefined;
  }
}

/** Due time, priority, list and reminders: what happens with an item. */
export function ItemFacts({ item, readOnly = false }: Props) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const lists = useLists();
  const now = useNow();
  const schedule = useSchedule(item);

  const day = item.allDay ? (item.dueDate ?? "") : item.dueAt ? dayIn(item.dueAt, now.zone) : "";
  const time = !item.allDay && item.dueAt ? timeIn(item.dueAt, now.zone) : "";
  const setDue = (nextDay: string, nextTime: string) =>
    void guard(
      patchItem(hub, item, { due: nextDay ? { day: nextDay, time: nextTime || null } : null }),
    );
  const dayField = useDraft(day, (next) => setDue(next, next ? time : ""), { enter: true });
  const timeField = useDraft(time, (next) => setDue(dayField.value || day, next), { enter: true });

  const [remind, setRemind] = useState("");
  const [remindError, setRemindError] = useState(false);
  const addOne = (event: FormEvent) => {
    event.preventDefault();
    const text = remind.trim();
    if (text === "") return;
    let at: Date | undefined;
    try {
      at = new Date(Date.now() + parseDuration(text));
    } catch {
      const when = parseWhen(text, now.today);
      if (when?.time) at = instantAt(when.day, when.time, now.zone);
    }
    setRemindError(!at);
    if (!at) return;
    setRemind("");
    void guard(addReminder(hub, item, at));
  };

  const stored = item.reminderOverride?.mode ?? "plan";
  const [pending, setPending] = useState<"custom">();
  const mode = pending ?? stored;
  const before = useCheckedDraft(
    (item.reminderOverride?.before ?? []).map(formatDuration).join(", "),
    durations,
    (parsed) => {
      setPending(undefined);
      void guard(patchItem(hub, item, { reminderOverride: { mode: "custom", before: parsed } }));
    },
  );
  const setMode = (next: string) => {
    if (next === "custom") return setPending("custom");
    setPending(undefined);
    void guard(patchItem(hub, item, { reminderOverride: next === "off" ? { mode: "off" } : null }));
  };

  return (
    <div className="col">
      <Card>
        <Field label={t("item.dueDate")}>
          <Input type="date" readOnly={readOnly} {...dayField} />
        </Field>
        {(dayField.value !== "" || day !== "") && (
          <>
            <Field label={t("item.dueTime")}>
              <Input type="time" readOnly={readOnly} {...timeField} />
            </Field>
            {!readOnly && (
              <Button variant="ghost" onClick={() => setDue("", "")}>
                {t("item.removeDue")}
              </Button>
            )}
          </>
        )}
        <Field label={t("item.priority")}>
          <Select
            value={item.priority}
            disabled={readOnly}
            onChange={(event) =>
              void guard(patchItem(hub, item, { priority: event.target.value as Priority }))
            }
          >
            {priorities.map((priority) => (
              <option key={priority} value={priority}>
                {t(`priority.${priority}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("item.list")}>
          <Select
            value={item.listId}
            disabled={readOnly}
            onChange={(event) => void guard(patchItem(hub, item, { listId: event.target.value }))}
          >
            {lists.map((list) => (
              <option key={list.id} value={list.id}>
                {list.name}
              </option>
            ))}
          </Select>
        </Field>
      </Card>

      {!readOnly && (
        <Card title={t("item.reminders")}>
          {item.reminders.map((reminder) => {
            const at = formatMoment(reminder.remindAt, now);
            return (
              <div className="between" key={reminder.id}>
                <span>{at}</span>
                <Button
                  variant="icon"
                  aria-label={t("item.removeReminder", { time: at })}
                  onClick={() => void guard(removeReminder(hub, item, reminder))}
                >
                  <Icon name="close" />
                </Button>
              </div>
            );
          })}
          <form onSubmit={addOne}>
            <Field
              label={t("item.remindMe")}
              error={remindError ? t("item.remindError") : undefined}
            >
              <Input
                value={remind}
                placeholder={t("item.remindHint")}
                onChange={(event) => setRemind(event.target.value)}
              />
            </Field>
          </form>
          <Field label={t("item.override")}>
            <Select value={mode} onChange={(event) => setMode(event.target.value)}>
              <option value="plan">{t("item.overridePlan")}</option>
              <option value="off">{t("item.overrideOff")}</option>
              <option value="custom">{t("item.overrideCustom")}</option>
            </Select>
          </Field>
          {mode === "custom" && (
            <Field
              label={t("item.overrideBefore")}
              error={before.invalid ? t("item.overrideError") : undefined}
            >
              <Input placeholder="1d, 2h" {...before.field} />
            </Field>
          )}
        </Card>
      )}

      {item.status === "open" && (
        <Card title={t("item.upcoming")}>
          {schedule?.length === 0 && <p className="text dim">{t("item.upcomingNone")}</p>}
          {schedule && schedule.length > 0 && (
            <div role="list" className="stack">
              {schedule.map((entry, index) => (
                <div role="listitem" className="kv" key={index}>
                  <span>{reminderLabel(entry.kind, entry.offset)}</span>
                  <span>{formatMoment(entry.fireAt, now)}</span>
                </div>
              ))}
            </div>
          )}
        </Card>
      )}
    </div>
  );
}
