import { useEffect } from "react";
import { useItem, useResolved } from "../hub/hooks.js";
import { useT } from "../i18n/index.js";
import { ItemFacts, ItemText } from "../pages/item/fields.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { useOpenItem, useSelection } from "./selection.js";

/** The selected item in a column next to the list. */
export function Details() {
  const t = useT();
  const { selected, select } = useSelection();
  const { id, key } = useResolved(selected);
  const item = useItem(id);
  const open = useOpenItem();

  useEffect(() => {
    if (id !== undefined && id !== selected) select(id);
  }, [id, selected, select]);

  if (!item) return null;
  return (
    <aside className="panel" aria-label={t("item.details")}>
      <div className="between">
        <Button onClick={() => open(item.id)}>
          <Icon name="open" />
          {t("item.openLarge")}
        </Button>
        <Button variant="icon" aria-label={t("common.close")} onClick={() => select(undefined)}>
          <Icon name="close" />
        </Button>
      </div>
      <ItemText key={`text:${key}`} item={item} compact />
      <ItemFacts key={`facts:${key}`} item={item} />
    </aside>
  );
}
