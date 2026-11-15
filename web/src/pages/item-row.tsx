import type { Item } from "plsremind";
import { memo, type MouseEvent } from "react";
import { useT } from "../i18n/index.js";
import type { Now } from "../model/groups.js";
import { routes } from "../shell/router.js";
import { formatDue } from "../time/format.js";
import { Row, Tag } from "../ui/layout.js";

export interface ItemRowProps {
  item: Item;
  now: Now;
  listName?: string | undefined;
  selected: boolean;
  onToggle: (item: Item) => void;
  onSelect: (item: Item) => void;
  onOpen: (item: Item) => void;
}

/** One item as a row: its checkbox, title, list, priority and due time. */
export const ItemRow = memo(function ItemRow(props: ItemRowProps) {
  const t = useT();
  const { item, now } = props;
  const done = item.status === "done";
  const late =
    !done &&
    (item.allDay && item.dueDate
      ? item.dueDate < now.today
      : item.dueAt !== null && item.dueAt.getTime() < now.instant.getTime());

  const onClick = (event: MouseEvent) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    // A click from the keyboard carries no click count.
    if (event.detail === 0) props.onOpen(item);
    else props.onSelect(item);
  };

  return (
    <Row selected={props.selected} done={done} onDoubleClick={() => props.onOpen(item)}>
      <input
        type="checkbox"
        className="check"
        checked={done}
        aria-label={t(done ? "items.reopen" : "items.complete", { title: item.title })}
        onChange={() => props.onToggle(item)}
      />
      <a className="t" href={routes.item(item.id)} onClick={onClick}>
        {item.title}
      </a>
      {props.listName !== undefined && <span className="meta only-wide">{props.listName}</span>}
      {item.priority === "high" && <Tag tone="high">{t("priority.high")}</Tag>}
      <span className={late ? "meta late" : "meta"}>{formatDue(item, now.zone, now.today)}</span>
    </Row>
  );
});
