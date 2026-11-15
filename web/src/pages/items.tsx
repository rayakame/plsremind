import type { Item, List } from "plsremind";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation } from "wouter";
import { useBar } from "../bar/context.js";
import { useHistory, useItems, useLists } from "../hub/hooks.js";
import { useNow } from "../hub/now.js";
import { useHub } from "../hub/session.js";
import { moveList, removeList, renameList, setDone } from "../hub/write.js";
import { useT, type Key } from "../i18n/index.js";
import { flatOrder, groupOpen, type GroupKey } from "../model/groups.js";
import { filterItems } from "../model/search.js";
import { useNotices } from "../shell/notices.js";
import { routes } from "../shell/router.js";
import { useOpenItem, useOrder, useSelection } from "../shell/selection.js";
import { Button, Segmented } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { useNarrow } from "../ui/layout-mode.js";
import { ConfirmDialog, NameDialog } from "../ui/name-dialog.js";
import { VirtualRows } from "../ui/virtual-rows.js";
import { ItemRow } from "./item-row.js";
import { NotFound } from "./not-found.js";

const groupLabels: Record<GroupKey, Key> = {
  overdue: "group.overdue",
  today: "group.today",
  week: "group.week",
  later: "group.later",
  none: "group.none",
};

const byList = new WeakMap<readonly Item[], Map<string, readonly Item[]>>();

/** The items of one list, the same array for as long as the items do not change. */
export function itemsOf(items: readonly Item[], listId: string): readonly Item[] {
  let lists = byList.get(items);
  if (!lists) byList.set(items, (lists = new Map()));
  let own = lists.get(listId);
  if (!own)
    lists.set(listId, (own = Object.freeze(items.filter((item) => item.listId === listId))));
  return own;
}

type Sort = "due" | "priority";
const none: readonly Item[] = Object.freeze([]);

function storedSort(listId: string): Sort {
  try {
    return sessionStorage.getItem(`plsremind.sort.${listId}`) === "priority" ? "priority" : "due";
  } catch {
    return "due";
  }
}

const rowKey = (item: Item) => item.id;

/** Open items in groups, with the handlers every such view shares. */
export function OpenItems(props: {
  items: readonly Item[];
  history: readonly Item[];
  sort: Sort;
  showList: boolean;
  /** A fixed text to narrow to, instead of what the bar holds. */
  filter?: string;
}) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const lists = useLists();
  const now = useNow();
  const narrow = useNarrow();
  const { selected, select } = useSelection();
  const open = useOpenItem();
  const bar = useBar();
  const filter = props.filter ?? bar.filter;
  const shown = filterItems(props.items, filter);
  const done = filter.trim() === "" ? none : filterItems(props.history, filter);
  const groups = groupOpen(shown, now, props.sort);

  const order = useMemo(
    () => [...flatOrder(groups), ...done.map((item) => item.id)],
    [groups, done],
  );
  useOrder(order);

  const names = useMemo(() => new Map(lists.map((list) => [list.id, list.name])), [lists]);
  const labelled = useMemo(
    () => [
      ...groups.map((group) => ({ ...group, label: t(groupLabels[group.key]) })),
      ...(done.length > 0 ? [{ key: "done", label: t("nav.done"), items: done }] : []),
    ],
    [groups, done, t],
  );
  const toggle = useCallback(
    (item: Item) => void guard(setDone(hub, item, item.status !== "done")),
    [guard, hub],
  );
  const pick = useCallback(
    (item: Item) => (narrow ? open(item.id) : select(item.id)),
    [narrow, open, select],
  );
  const openItem = useCallback((item: Item) => open(item.id), [open]);
  const { showList } = props;
  const render = useCallback(
    (item: Item) => (
      <ItemRow
        item={item}
        now={now}
        listName={showList ? (names.get(item.listId) ?? "") : undefined}
        selected={item.id === selected}
        onToggle={toggle}
        onSelect={pick}
        onOpen={openItem}
      />
    ),
    [now, showList, names, selected, toggle, pick, openItem],
  );

  if (labelled.length === 0) {
    return <p className="lead">{t(filter.trim() === "" ? "items.empty" : "items.noMatch")}</p>;
  }
  return (
    <VirtualRows
      label={t("items.label")}
      groups={labelled}
      rowKey={rowKey}
      render={render}
      selected={selected}
    />
  );
}

export function AllItemsPage() {
  const t = useT();
  const items = useItems();
  const history = useHistory();
  return (
    <>
      <h1 className="h1">{t("nav.all")}</h1>
      <OpenItems items={items} history={history} sort="due" showList />
    </>
  );
}

function ListHeader(props: { list: List; sort: Sort; onSort: (sort: Sort) => void }) {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const lists = useLists();
  const [, navigate] = useLocation();
  const [dialog, setDialog] = useState<"rename" | "delete">();
  const { list } = props;
  const at = lists.findIndex((one) => one.id === list.id);

  return (
    <div className="between">
      <h1 className="h1">{list.name}</h1>
      <div className="line">
        <Segmented
          label={t("list.sort")}
          value={props.sort}
          options={[
            { value: "due", label: t("list.sortDue") },
            { value: "priority", label: t("list.sortPriority") },
          ]}
          onChange={props.onSort}
        />
        <Button
          variant="icon"
          aria-label={t("common.moveUp")}
          disabled={at <= 0}
          onClick={() => void guard(moveList(hub, list, -1))}
        >
          <Icon name="up" />
        </Button>
        <Button
          variant="icon"
          aria-label={t("common.moveDown")}
          disabled={at < 0 || at >= lists.length - 1}
          onClick={() => void guard(moveList(hub, list, 1))}
        >
          <Icon name="down" />
        </Button>
        {!list.isInbox && <Button onClick={() => setDialog("rename")}>{t("common.rename")}</Button>}
        {!list.isInbox && (
          <Button variant="icon" aria-label={t("list.delete")} onClick={() => setDialog("delete")}>
            <Icon name="trash" />
          </Button>
        )}
      </div>
      {dialog === "rename" && (
        <NameDialog
          title={t("common.rename")}
          initial={list.name}
          submit={t("common.save")}
          onSubmit={(name) => void guard(renameList(hub, list, name))}
          onClose={() => setDialog(undefined)}
        />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("list.delete")}
          text={t("list.deleteText")}
          confirm={t("common.delete")}
          onConfirm={() => {
            void guard(removeList(hub, list));
            navigate(routes.items);
          }}
          onClose={() => setDialog(undefined)}
        />
      )}
    </div>
  );
}

export function ListPage(props: { id: string }) {
  const lists = useLists();
  const items = useItems();
  const history = useHistory();
  const list = lists.find((one) => one.id === props.id);
  const [sort, setSort] = useState<Sort>(() => storedSort(props.id));
  useEffect(() => setSort(storedSort(props.id)), [props.id]);

  if (!list) return <NotFound />;
  const change = (next: Sort) => {
    setSort(next);
    try {
      sessionStorage.setItem(`plsremind.sort.${list.id}`, next);
    } catch {
      // The order then lasts until the page changes.
    }
  };
  return (
    <>
      <ListHeader list={list} sort={sort} onSort={change} />
      <OpenItems
        items={itemsOf(items, list.id)}
        history={itemsOf(history, list.id)}
        sort={sort}
        showList={false}
      />
    </>
  );
}
