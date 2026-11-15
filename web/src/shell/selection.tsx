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
  /** True if the item page that is open was reached from a view of this page load. */
  entered: boolean;
  setEntered: (entered: boolean) => void;
}

const none: readonly string[] = Object.freeze([]);
const SelectionContext = createContext<Selection | undefined>(undefined);

export function SelectionProvider(props: { children: ReactNode }) {
  const [selected, select] = useState<string>();
  const [order, setOrder] = useState(none);
  const [listed, setListed] = useState(false);
  const [entered, setEntered] = useState(false);
  const value = useMemo(
    () => ({ selected, select, order, listed, setOrder, setListed, entered, setEntered }),
    [selected, order, listed, entered],
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
  const { select, setEntered } = useSelection();
  const [, navigate] = useLocation();
  return useCallback(
    (id) => {
      select(undefined);
      setEntered(true);
      navigate(routes.item(id));
    },
    [navigate, select, setEntered],
  );
}

/** Leaves an item's page: back to where one came from, or to all items after a direct visit. */
export function useLeaveItem(): () => void {
  const { entered, setEntered } = useSelection();
  const [, navigate] = useLocation();
  return useCallback(() => {
    setEntered(false);
    if (entered) history.back();
    else navigate(routes.items);
  }, [entered, navigate, setEntered]);
}
