import type { Item } from "plsremind";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useBar } from "../bar/context.js";
import { useHistory, useLists } from "../hub/hooks.js";
import { useNow } from "../hub/now.js";
import { useOlderDone } from "../hub/older.js";
import { useHub } from "../hub/session.js";
import { setDone } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { doneGroups, flatOrder } from "../model/groups.js";
import { filterItems } from "../model/search.js";
import { useNotices } from "../shell/notices.js";
import { useOpenItem, useSelection } from "../shell/selection.js";
import { Button, Field, Select } from "../ui/controls.js";
import { useNarrow } from "../ui/layout-mode.js";
import { VirtualRows } from "../ui/virtual-rows.js";
import { ItemRow } from "./item-row.js";

const rowKey = (item: Item) => item.id;

export function DonePage() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const lists = useLists();
  const history = useHistory();
  const older = useOlderDone();
  const now = useNow();
  const narrow = useNarrow();
  const { filter } = useBar();
  const { selected, select, setOrder } = useSelection();
  const open = useOpenItem();
  const [listId, setListId] = useState("");

  const all = useMemo(
    () => (older.items.length === 0 ? history : [...history, ...older.items]),
    [history, older.items],
  );
  const ofList = useMemo(
    () => (listId === "" ? all : all.filter((item) => item.listId === listId)),
    [all, listId],
  );
  const shown = filterItems(ofList, filter);
  const groups = doneGroups(shown, now);
  const labelled = useMemo(
    () => groups.map((group) => ({ ...group, label: t(`done.${group.key}`) })),
    [groups, t],
  );
  const order = useMemo(() => flatOrder(groups), [groups]);
  useEffect(() => setOrder(order), [order, setOrder]);

  const names = useMemo(() => new Map(lists.map((list) => [list.id, list.name])), [lists]);
  const toggle = useCallback((item: Item) => void guard(setDone(hub, item, false)), [guard, hub]);
  const pick = useCallback(
    (item: Item) => (narrow ? open(item.id) : select(item.id)),
    [narrow, open, select],
  );
  const openItem = useCallback((item: Item) => open(item.id), [open]);
  const render = useCallback(
    (item: Item) => (
      <ItemRow
        item={item}
        now={now}
        listName={names.get(item.listId) ?? ""}
        selected={item.id === selected}
        onToggle={toggle}
        onSelect={pick}
        onOpen={openItem}
      />
    ),
    [now, names, selected, toggle, pick, openItem],
  );

  return (
    <>
      <div className="between">
        <h1 className="h1">{t("nav.done")}</h1>
        <div className="line">
          <Field label={t("item.list")}>
            <Select value={listId} onChange={(event) => setListId(event.target.value)}>
              <option value="">{t("done.allLists")}</option>
              {lists.map((list) => (
                <option key={list.id} value={list.id}>
                  {list.name}
                </option>
              ))}
            </Select>
          </Field>
        </div>
      </div>
      {labelled.length === 0 ? (
        <p className="lead">{t(all.length === 0 ? "done.empty" : "items.noMatch")}</p>
      ) : (
        <VirtualRows
          label={t("nav.done")}
          groups={labelled}
          rowKey={rowKey}
          render={render}
          selected={selected}
        />
      )}
      {older.more && (
        <div className="line">
          <Button disabled={older.loading} onClick={() => void guard(older.loadMore())}>
            {t("done.loadOlder")}
          </Button>
        </div>
      )}
    </>
  );
}
