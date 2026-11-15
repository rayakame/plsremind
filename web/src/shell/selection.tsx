import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useLocation } from "wouter";
import { routes } from "./router.js";

interface Selection {
  selected: string | undefined;
  select: (id: string | undefined) => void;
  /** The ids of the current view in display order, for moving through them. */
  order: readonly string[];
  setOrder: (order: readonly string[]) => void;
}

const none: readonly string[] = Object.freeze([]);
const SelectionContext = createContext<Selection | undefined>(undefined);

export function SelectionProvider(props: { children: ReactNode }) {
  const [selected, select] = useState<string>();
  const [order, setOrder] = useState(none);
  const value = useMemo(() => ({ selected, select, order, setOrder }), [selected, order]);
  return <SelectionContext.Provider value={value}>{props.children}</SelectionContext.Provider>;
}

export function useSelection(): Selection {
  const selection = useContext(SelectionContext);
  if (!selection) throw new Error("no selection");
  return selection;
}

/** Opens an item's own page, without the detail column next to it. */
export function useOpenItem(): (id: string) => void {
  const { select } = useSelection();
  const [, navigate] = useLocation();
  return useCallback(
    (id) => {
      select(undefined);
      navigate(routes.item(id));
    },
    [navigate, select],
  );
}
