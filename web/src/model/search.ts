import type { Item } from "plsremind";

const folded = new WeakMap<Item, string>();
const memo = new WeakMap<readonly Item[], { text: string; result: readonly Item[] }>();

/** Lower case without accents, so that "uber" finds "Über". */
export function fold(text: string): string {
  return text.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

function haystack(item: Item): string {
  let text = folded.get(item);
  if (text === undefined) folded.set(item, (text = fold(`${item.title}\n${item.note}`)));
  return text;
}

/** The items whose title or note holds every word of the text. */
export function filterItems<T extends Item>(items: readonly T[], text: string): readonly T[] {
  const words = fold(text).split(/\s+/).filter(Boolean);
  if (words.length === 0) return items;
  const key = words.join(" ");
  const before = memo.get(items);
  if (before?.text === key) return before.result as readonly T[];
  const result = Object.freeze(
    items.filter((item) => {
      const hay = haystack(item);
      return words.every((word) => hay.includes(word));
    }),
  );
  memo.set(items, { text: key, result });
  return result;
}
