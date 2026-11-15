import type { Item } from "plsremind";

const folded = new WeakMap<Item, string>();
const memo = new WeakMap<readonly Item[], { text: string; result: readonly Item[] }>();

/** Lower case without accents, so that "uber" finds "Über". */
export function fold(text: string): string {
  return text.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

/** The words of a text, each with a space in front, so that a word's start can be searched for. */
function words(text: string): string {
  return ` ${fold(text).replace(/[^\p{L}\p{N}]+/gu, " ")}`;
}

function haystack(item: Item): string {
  let text = folded.get(item);
  if (text === undefined) folded.set(item, (text = words(`${item.title} ${item.note}`)));
  return text;
}

/** The items whose title or note has, for every word of the text, a word that starts with it. */
export function filterItems<T extends Item>(items: readonly T[], text: string): readonly T[] {
  const wanted = words(text).split(" ").filter(Boolean);
  if (wanted.length === 0) return items;
  const key = wanted.join(" ");
  const before = memo.get(items);
  if (before?.text === key) return before.result as readonly T[];
  const result = Object.freeze(
    items.filter((item) => {
      const hay = haystack(item);
      return wanted.every((word) => hay.includes(` ${word}`));
    }),
  );
  memo.set(items, { text: key, result });
  return result;
}
