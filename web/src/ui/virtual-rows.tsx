import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { rowHeight } from "./layout-mode.js";

type Line<T> = { head: string; key: string } | { item: T; key: string };

/** Grouped rows of which only the visible ones are in the document. */
export function VirtualRows<T>(props: {
  label: string;
  groups: readonly { key: string; label: string; items: readonly T[] }[];
  rowKey: (item: T) => string;
  render: (item: T) => ReactNode;
  selected?: string | undefined;
}) {
  const { groups, rowKey } = props;
  const scroll = useRef<HTMLDivElement>(null);
  const lines = useMemo(() => {
    const all: Line<T>[] = [];
    for (const group of groups) {
      all.push({ head: group.label, key: `head:${group.key}` });
      for (const item of group.items) all.push({ item, key: rowKey(item) });
    }
    return all;
  }, [groups, rowKey]);
  const height = useMemo(rowHeight, []);
  const virtual = useVirtualizer({
    count: lines.length,
    getScrollElement: () => scroll.current,
    estimateSize: () => height,
    getItemKey: (index) => lines[index]!.key,
    overscan: 8,
  });

  const selected = props.selected;
  useEffect(() => {
    if (selected === undefined) return;
    const index = lines.findIndex((line) => line.key === selected);
    if (index >= 0) virtual.scrollToIndex(index, { align: "auto" });
    // Only a new selection scrolls; new data must not move the list.
  }, [selected]);

  return (
    <div className="scroll" ref={scroll}>
      <div
        className="vlist"
        role="list"
        aria-label={props.label}
        style={{ height: virtual.getTotalSize() }}
      >
        {virtual.getVirtualItems().map((row) => {
          const line = lines[row.index]!;
          return (
            <div
              key={row.key}
              className="vrow"
              role={"head" in line ? "presentation" : "listitem"}
              aria-current={"item" in line && line.key === selected ? "true" : undefined}
              style={{ transform: `translateY(${row.start}px)` }}
            >
              {"head" in line ? (
                <div className="vhead">
                  <h2 className="label">{line.head}</h2>
                </div>
              ) : (
                props.render(line.item)
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
