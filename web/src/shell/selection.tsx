import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useLocation } from "wouter";
import { routes } from "./router.js";

interface Selection {
  selected: string | undefined;
  select: (id: string | undefined) => void;
  /** The ids of the last list view in display order, for moving through them. */
  order: readonly string[];
  /** True while a view that shows this order is on the page. */
  listed: boolean;
  setOrder: (order: readonly string[]) => void;
  setListed: (listed: boolean) => void;
}

const none: readonly string[] = Object.freeze([]);
const SelectionContext = createContext<Selection | undefined>(undefined);

export function SelectionProvider(props: { children: ReactNode }) {
  const [selected, select] = useState<string>();
  const [order, setOrder] = useState(none);
  const [listed, setListed] = useState(false);
  const value = useMemo(
    () => ({ selected, select, order, listed, setOrder, setListed }),
    [selected, order, listed],
  );
  return <SelectionContext.Provider value={value}>{props.children}</SelectionContext.Provider>;
}

export function useSelection(): Selection {
  const selection = useContext(SelectionContext);
  if (!selection) throw new Error("no selection");
  return selection;
}

/** Tells the keys and the item page which items the current view shows, in which order. */
export function useOrder(order: readonly string[]): void {
  const { setOrder, setListed, select } = useSelection();
  useEffect(() => setOrder(order), [order, setOrder]);
  useEffect(() => {
    setListed(true);
    return () => {
      setListed(false);
      select(undefined);
    };
  }, [setListed, select]);
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
