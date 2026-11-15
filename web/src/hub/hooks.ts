import type { CacheKind, ClientStatus, Item, List, ReminderPlan, User, UserState } from "plsremind";
import { useCallback, useRef, useSyncExternalStore } from "react";
import { useHub } from "./session.js";

const none: readonly never[] = Object.freeze([]);

function useKinds<T>(kinds: readonly CacheKind[], read: (state: UserState | undefined) => T): T {
  const { view, userId } = useHub();
  const subscribe = useCallback(
    (notify: () => void) =>
      view.subscribe((change) => {
        if (change.userId === userId && change.kinds.some((kind) => kinds.includes(kind))) notify();
      }),
    [view, userId, kinds],
  );
  return useSyncExternalStore(subscribe, () => read(view.user(userId)));
}

const profileKinds = ["profile"] as const;
const listKinds = ["lists"] as const;
const itemKinds = ["items"] as const;
const historyKinds = ["history"] as const;
const planKinds = ["plans"] as const;
const bothKinds = ["items", "history"] as const;

export function useProfile(): User {
  const last = useRef<User | undefined>(undefined);
  const profile = useKinds(profileKinds, (state) => state?.profile as User | undefined);
  if (profile) last.current = profile;
  if (!last.current) throw new Error("no profile");
  return last.current;
}

export function useLists(): readonly List[] {
  return useKinds(listKinds, (state) => state?.lists() ?? none);
}

export function useItems(): readonly Item[] {
  return useKinds(itemKinds, (state) => state?.items() ?? none);
}

export function useHistory(): readonly Item[] {
  return useKinds(historyKinds, (state) => state?.history() ?? none);
}

export function usePlans(): readonly ReminderPlan[] {
  return useKinds(planKinds, (state) => state?.plans() ?? none);
}

export function useItem(id: string | undefined): Item | undefined {
  return useKinds(bothKinds, (state) => (id === undefined ? undefined : state?.item(id)));
}

const connectionEvents = ["ready", "resumed", "disconnected", "closed"] as const;

export function useConnection(): ClientStatus {
  const { client } = useHub();
  const subscribe = useCallback(
    (notify: () => void) => {
      const stops = connectionEvents.map((name) => client.on(name, notify));
      return () => stops.forEach((stop) => stop());
    },
    [client],
  );
  return useSyncExternalStore(subscribe, () => client.status);
}
