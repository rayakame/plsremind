import type { Priority } from "plsremind";
import { useState, type FormEvent } from "react";
import { useLists, useResolved } from "../hub/hooks.js";
import type { ItemDraft } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { Button, Field, Input, Select } from "../ui/controls.js";
import { Dialog } from "../ui/dialog.js";

const priorities: readonly Priority[] = ["high", "normal", "low"];

/** A small form for a new item with more than a title. */
export function QuickForm(props: {
  listId: string | undefined;
  day: string | null;
  onCreate: (draft: ItemDraft) => void;
  onClose: () => void;
}) {
  const t = useT();
  const lists = useLists();
  const inbox = lists.find((list) => list.isInbox)?.id ?? "";
  const [title, setTitle] = useState("");
  const [day, setDay] = useState(props.day ?? "");
  const [time, setTime] = useState("");
  const [priority, setPriority] = useState<Priority>("normal");
  const [picked, setPicked] = useState(props.listId ?? inbox);
  const listId = useResolved(picked).id ?? picked;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = title.trim();
    if (trimmed === "") return;
    props.onCreate({
      title: trimmed,
      listId,
      priority: priority === "normal" ? undefined : priority,
      due: day ? { day, time: time || null } : undefined,
    });
    props.onClose();
  };

  return (
    <Dialog title={t("bar.newItem")} onClose={props.onClose}>
      <form className="stack" onSubmit={submit}>
        <Field label={t("item.title")}>
          <Input value={title} onChange={(event) => setTitle(event.target.value)} required />
        </Field>
        <Field label={t("item.dueDate")}>
          <Input type="date" value={day} onChange={(event) => setDay(event.target.value)} />
        </Field>
        {day !== "" && (
          <Field label={t("item.dueTime")}>
            <Input type="time" value={time} onChange={(event) => setTime(event.target.value)} />
          </Field>
        )}
        <Field label={t("item.priority")}>
          <Select
            value={priority}
            onChange={(event) => setPriority(event.target.value as Priority)}
          >
            {priorities.map((one) => (
              <option key={one} value={one}>
                {t(`priority.${one}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("item.list")}>
          <Select value={listId} onChange={(event) => setPicked(event.target.value)}>
            {lists.map((list) => (
              <option key={list.id} value={list.id}>
                {list.name}
              </option>
            ))}
          </Select>
        </Field>
        <div className="line">
          <Button variant="primary" type="submit">
            {t("common.create")}
          </Button>
          <Button variant="ghost" onClick={props.onClose}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
