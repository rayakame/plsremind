import { useSearch } from "wouter";
import { useHistory, useItems } from "../hub/hooks.js";
import { useT } from "../i18n/index.js";
import { OpenItems } from "./items.js";

/** Open and done items that match a text, as the find command shows them. */
export function FindPage() {
  const t = useT();
  const text = new URLSearchParams(useSearch()).get("q") ?? "";
  const items = useItems();
  const history = useHistory();
  return (
    <>
      <h1 className="h1">{t("find.title", { text })}</h1>
      <OpenItems items={items} history={history} sort="due" showList filter={text} />
    </>
  );
}
