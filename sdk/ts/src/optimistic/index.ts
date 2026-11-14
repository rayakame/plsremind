import type {
  Cache,
  CacheChange,
  CacheKind,
  Client,
  Item,
  List,
  Priority,
  Profile,
  ReminderPlan,
  UserState,
} from "../index.js";

/** One change to show before the hub confirmed it. */
export type Change =
  | { readonly kind: "item"; readonly create: Item }
  | { readonly kind: "item"; readonly id: string; readonly update: (item: Item) => Item }
  | { readonly kind: "item"; readonly id: string; readonly remove: true }
  | { readonly kind: "list"; readonly create: List }
  | { readonly kind: "list"; readonly id: string; readonly update: (list: List) => List }
  | { readonly kind: "list"; readonly id: string; readonly remove: true }
  | {
      readonly kind: "plan";
      readonly priority: Priority;
      readonly update: (plan: ReminderPlan) => ReminderPlan;
    }
  | { readonly kind: "profile"; readonly update: (profile: Profile) => Profile };

/** The cache with changes that are still on their way laid over it. */
export interface OptimisticView extends Cache {
  /**
   * Shows the changes at once, runs the call and returns its result. The call gets a nonce
   * to pass on to REST. If it rejects, the changes are taken back and the error is rethrown.
   */
  apply<T>(
    changes: readonly Change[],
    call: (nonce: string) => Promise<T>,
    options?: { user?: string },
  ): Promise<T>;
  /** An id for an object that is created ahead of the hub. */
  tempId(): string;
  /** The real id behind a temporary one, once it is known. */
  resolveId(tempId: string): string | undefined;
}

type Kind = Change["kind"];

interface Entry {
  readonly kind: Kind;
  id: string;
  readonly create: Item | List | undefined;
  readonly update: ((value: never) => unknown) | undefined;
  readonly batch: Batch;
}

interface Batch {
  readonly nonce: string;
  readonly userId: string;
  readonly entries: Set<Entry>;
  answered: boolean;
  timer: ReturnType<typeof setTimeout> | undefined;
}

const settleAfter = 5000;
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
const shown: Record<Kind, readonly CacheKind[]> = {
  item: ["items", "history"],
  list: ["lists"],
  plan: ["plans"],
  profile: ["profile"],
};
const views = new WeakMap<Client, OptimisticView>();

function randomText(length: number): string {
  const bytes = crypto.getRandomValues(new Uint8Array(length));
  let text = "";
  for (const byte of bytes) text += alphabet[byte % alphabet.length];
  return text;
}

function same(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a instanceof Date && b instanceof Date) return a.getTime() === b.getTime();
  if (a === null || b === null || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  return keys.every(
    (key) =>
      key in b && same((a as Record<string, unknown>)[key], (b as Record<string, unknown>)[key]),
  );
}

function eventKind(name: string): Kind | undefined {
  if (name.startsWith("item.")) return "item";
  if (name.startsWith("list.")) return "list";
  if (name === "plan.updated") return "plan";
  if (name === "user.updated") return "profile";
  return undefined;
}

function completedAt(item: Item): number {
  return item.completedAt?.getTime() ?? Number.POSITIVE_INFINITY;
}

/** The array from before if nothing in it changed, so that readers see the same snapshot. */
function kept<T>(before: readonly T[] | undefined, now: T[]): readonly T[] {
  if (before && before.length === now.length && before.every((value, i) => value === now[i])) {
    return before;
  }
  return Object.freeze(now);
}

interface Memo {
  base: unknown;
  stamp: number;
  result: unknown;
}

interface ItemsState {
  baseItems: readonly Item[];
  baseHistory: readonly Item[];
  stamp: number;
  finals: Map<string, Item | null>;
  items: readonly Item[];
  history: readonly Item[];
}

interface ListsState {
  base: readonly List[];
  stamp: number;
  finals: Map<string, List | null>;
  lists: readonly List[];
}

interface PlansState {
  base: readonly ReminderPlan[];
  stamp: number;
  finals: Map<string, ReminderPlan>;
  plans: readonly ReminderPlan[];
}

class ViewUser implements UserState {
  readonly entries: Entry[] = [];
  readonly #base: UserState;
  readonly #keepsHistory: () => boolean;
  readonly #stamps = new Map<string, number>();
  readonly #memos = new Map<string, Memo>();
  #items: ItemsState | undefined;
  #lists: ListsState | undefined;
  #plans: PlansState | undefined;

  constructor(base: UserState, keepsHistory: () => boolean) {
    this.#base = base;
    this.#keepsHistory = keepsHistory;
  }

  get userId(): string {
    return this.#base.userId;
  }

  get historyCursor(): string | null {
    return this.#base.historyCursor;
  }

  touch(kind: Kind, id: string): void {
    this.#stamps.set(kind, (this.#stamps.get(kind) ?? 0) + 1);
    const key = `${kind}:${id}`;
    this.#stamps.set(key, (this.#stamps.get(key) ?? 0) + 1);
  }

  forget(kind: Kind, id: string): void {
    this.#stamps.delete(`${kind}:${id}`);
    this.#memos.delete(`${kind}:${id}`);
  }

  #of(kind: Kind): Entry[] {
    return this.entries.filter((entry) => entry.kind === kind);
  }

  #overlay<T>(kind: Kind, confirmed: (id: string) => T | undefined): Map<string, T | null> {
    const finals = new Map<string, T | null>();
    for (const entry of this.#of(kind)) {
      const current = finals.has(entry.id) ? finals.get(entry.id) : confirmed(entry.id);
      if (entry.create) finals.set(entry.id, entry.create as unknown as T);
      else if (!entry.update) finals.set(entry.id, null);
      else if (current) {
        try {
          finals.set(entry.id, (entry.update as (value: T) => T)(current));
        } catch (error) {
          console.error("plsremind: a pending change could not be applied", error);
        }
      }
    }
    for (const [id, result] of finals) {
      const key = `${kind}:${id}`;
      const base = confirmed(id);
      const stamp = this.#stamps.get(key) ?? 0;
      const memo = this.#memos.get(key);
      if (memo && memo.base === base && memo.stamp === stamp) {
        finals.set(id, memo.result as T | null);
      } else {
        this.#memos.set(key, { base, stamp, result });
      }
    }
    return finals;
  }

  #itemsState(): ItemsState | undefined {
    const stamp = this.#stamps.get("item") ?? 0;
    const baseItems = this.#base.items();
    const baseHistory = this.#base.history();
    const cached = this.#items;
    if (
      cached &&
      cached.stamp === stamp &&
      cached.baseItems === baseItems &&
      cached.baseHistory === baseHistory
    ) {
      return cached;
    }
    if (!this.entries.some((entry) => entry.kind === "item")) {
      this.#items = undefined;
      return undefined;
    }
    const finals = this.#overlay<Item>("item", (id) => this.#base.item(id));
    const items: Item[] = [];
    for (const item of baseItems) {
      const final = finals.has(item.id) ? finals.get(item.id) : item;
      if (final && final.status === "open") items.push(final);
    }
    let history: Item[] = [];
    for (const item of baseHistory) {
      const final = finals.has(item.id) ? finals.get(item.id) : item;
      if (final && final.status === "done") history.push(final);
    }
    for (const [id, final] of finals) {
      if (!final) continue;
      const before = this.#base.item(id)?.status;
      if (final.status === "open" && before !== "open") {
        items.push(final);
      } else if (final.status === "done" && before !== "done" && this.#keepsHistory()) {
        const at = completedAt(final);
        const index = history.findIndex((other) => completedAt(other) <= at);
        history.splice(index < 0 ? history.length : index, 0, final);
      }
    }
    if (!this.#keepsHistory()) history = [];
    this.#items = {
      baseItems,
      baseHistory,
      stamp,
      finals,
      items: kept(cached?.items, items),
      history: kept(cached?.history, history),
    };
    return this.#items;
  }

  items(): readonly Item[] {
    return this.#itemsState()?.items ?? this.#base.items();
  }

  history(): readonly Item[] {
    return this.#itemsState()?.history ?? this.#base.history();
  }

  item(id: string): Item | undefined {
    const state = this.#itemsState();
    if (!state?.finals.has(id)) return this.#base.item(id);
    const final = state.finals.get(id);
    if (!final || (final.status === "done" && !this.#keepsHistory())) return undefined;
    return final;
  }

  #listsState(): ListsState | undefined {
    const stamp = this.#stamps.get("list") ?? 0;
    const base = this.#base.lists();
    const cached = this.#lists;
    if (cached && cached.stamp === stamp && cached.base === base) return cached;
    if (!this.entries.some((entry) => entry.kind === "list")) {
      this.#lists = undefined;
      return undefined;
    }
    const finals = this.#overlay<List>("list", (id) => this.#base.list(id));
    const lists: List[] = [];
    for (const list of base) {
      const final = finals.has(list.id) ? finals.get(list.id) : list;
      if (final) lists.push(final);
    }
    for (const [id, final] of finals) {
      if (final && !this.#base.list(id)) lists.push(final);
    }
    lists.sort((a, b) => a.position - b.position);
    this.#lists = { base, stamp, finals, lists: kept(cached?.lists, lists) };
    return this.#lists;
  }

  lists(): readonly List[] {
    return this.#listsState()?.lists ?? this.#base.lists();
  }

  list(id: string): List | undefined {
    const state = this.#listsState();
    if (!state?.finals.has(id)) return this.#base.list(id);
    return state.finals.get(id) ?? undefined;
  }

  #plansState(): PlansState | undefined {
    const stamp = this.#stamps.get("plan") ?? 0;
    const base = this.#base.plans();
    const cached = this.#plans;
    if (cached && cached.stamp === stamp && cached.base === base) return cached;
    if (!this.entries.some((entry) => entry.kind === "plan")) {
      this.#plans = undefined;
      return undefined;
    }
    const finals = this.#overlay<ReminderPlan>("plan", (priority) =>
      this.#base.plan(priority as Priority),
    ) as Map<string, ReminderPlan>;
    const plans = base.map((plan) => finals.get(plan.priority) ?? plan);
    this.#plans = { base, stamp, finals, plans: kept(cached?.plans, plans) };
    return this.#plans;
  }

  plans(): readonly ReminderPlan[] {
    return this.#plansState()?.plans ?? this.#base.plans();
  }

  plan(priority: Priority): ReminderPlan | undefined {
    return this.#plansState()?.finals.get(priority) ?? this.#base.plan(priority);
  }

  get profile(): Profile | undefined {
    if (!this.entries.some((entry) => entry.kind === "profile")) return this.#base.profile;
    const finals = this.#overlay<Profile>("profile", () => this.#base.profile);
    return finals.get("") ?? this.#base.profile;
  }

  /** The confirmed object an entry is about, if there is one. */
  confirmed(entry: Entry): unknown {
    if (entry.kind === "item") return this.#base.item(entry.id);
    if (entry.kind === "list") return this.#base.list(entry.id);
    if (entry.kind === "plan") return this.#base.plan(entry.id as Priority);
    return this.#base.profile;
  }
}

/** A view of the client's cache that shows changes before the hub confirmed them. */
export function optimistic(client: Client): OptimisticView {
  let view = views.get(client);
  if (!view) views.set(client, (view = build(client)));
  return view;
}

function build(client: Client): OptimisticView {
  const cache = client.cache;
  const users = new Map<string, ViewUser>();
  const listeners = new Set<(change: CacheChange) => void>();
  const resolved = new Map<string, string>();
  const answered = new Map<string, string>();
  const drafts = new Set<string>();
  let list: { base: readonly UserState[]; users: readonly UserState[] } | undefined;

  const keepsHistory = () => client.intents.includes("history");

  function userOf(userId: string | undefined): ViewUser | undefined {
    const base = cache.user(userId);
    if (!base) return undefined;
    let user = users.get(base.userId);
    if (!user) users.set(base.userId, (user = new ViewUser(base, keepsHistory)));
    return user;
  }

  function notify(change: CacheChange): void {
    for (const listener of [...listeners]) {
      try {
        listener(change);
      } catch (error) {
        console.error("plsremind: a view listener threw", error);
      }
    }
  }

  function remove(entry: Entry, kinds: Set<CacheKind>): void {
    const batch = entry.batch;
    if (!batch.entries.delete(entry)) return;
    if (batch.entries.size === 0) clearTimeout(batch.timer);
    const user = users.get(batch.userId);
    if (user) {
      user.entries.splice(user.entries.indexOf(entry), 1);
      user.touch(entry.kind, entry.id);
    }
    if (entry.create) {
      drafts.delete(entry.id);
      const realId = answered.get(entry.id);
      answered.delete(entry.id);
      if (realId !== undefined) resolved.set(entry.id, realId);
      // Changes made to the draft now belong to the object that took its place.
      for (const other of (realId !== undefined && user?.entries) || []) {
        if (other.id === entry.id && other.kind === entry.kind) {
          other.id = realId!;
          user!.touch(other.kind, realId!);
        }
      }
    }
    if (user && !user.entries.some((other) => other.kind === entry.kind && other.id === entry.id)) {
      user.forget(entry.kind, entry.id);
    }
    for (const kind of shown[entry.kind]) kinds.add(kind);
  }

  /** True if an older change to the same object is still pending. */
  function behind(user: ViewUser, entry: Entry): boolean {
    for (const other of user.entries) {
      if (other === entry) return false;
      if (other.kind === entry.kind && other.id === entry.id) return true;
    }
    return false;
  }

  /** True once the confirmed state shows what the entry wanted. */
  function arrived(user: ViewUser, entry: Entry): boolean {
    if (entry.create) {
      const realId = answered.get(entry.id);
      return realId !== undefined && user.confirmed({ ...entry, id: realId }) !== undefined;
    }
    if (behind(user, entry)) return false;
    const confirmed = user.confirmed(entry);
    if (!entry.update) return confirmed === undefined;
    return (
      confirmed !== undefined &&
      same((entry.update as (v: unknown) => unknown)(confirmed), confirmed)
    );
  }

  function vanished(user: ViewUser, entry: Entry): boolean {
    if (entry.create || entry.kind === "plan" || entry.kind === "profile") return false;
    return user.confirmed(entry) === undefined && !drafts.has(entry.id);
  }

  cache.subscribe((change) => {
    const kinds = new Set<CacheKind>(change.kinds);
    const user = users.get(change.userId);
    if (user && !cache.user(change.userId)) {
      for (const entry of [...user.entries]) remove(entry, kinds);
      users.delete(change.userId);
    } else if (user) {
      const cause = change.cause;
      const kind = cause ? eventKind(cause.name) : undefined;
      let born = cause?.name.endsWith(".created") ? cause.id : undefined;
      for (const entry of [...user.entries]) {
        if (cause && kind === entry.kind && cause.nonce === entry.batch.nonce) {
          const known = answered.get(entry.id);
          if (entry.create && born !== undefined && (known === undefined || known === born)) {
            answered.set(entry.id, born);
            born = undefined;
            remove(entry, kinds);
            continue;
          }
          if (!entry.create && (kind === "profile" || cause.id === entry.id)) {
            remove(entry, kinds);
            continue;
          }
        }
        if (vanished(user, entry) || (entry.batch.answered && arrived(user, entry))) {
          remove(entry, kinds);
        }
      }
    }
    notify({ userId: change.userId, kinds: [...kinds], cause: change.cause });
  });

  async function apply<T>(
    changes: readonly Change[],
    call: (nonce: string) => Promise<T>,
    options: { user?: string } = {},
  ): Promise<T> {
    const principal = client.principal;
    const own = principal && "userId" in principal ? principal.userId : undefined;
    const userId = options.user === undefined || options.user === "@me" ? own : options.user;
    if (userId === undefined) {
      throw new TypeError("Say which user the changes are for: pass { user }.");
    }
    const batch: Batch = {
      nonce: randomText(16),
      userId,
      entries: new Set(),
      answered: false,
      timer: undefined,
    };
    const user = userOf(userId);
    const kinds = new Set<CacheKind>();
    const created: Entry[] = [];
    if (user) {
      for (const change of changes) {
        const entry: Entry = {
          kind: change.kind,
          id:
            "create" in change
              ? change.create.id
              : change.kind === "plan"
                ? change.priority
                : change.kind === "profile"
                  ? ""
                  : change.id,
          create: "create" in change ? change.create : undefined,
          update: "update" in change ? (change.update as Entry["update"]) : undefined,
          batch,
        };
        if (entry.create) {
          drafts.add(entry.id);
          created.push(entry);
        }
        batch.entries.add(entry);
        user.entries.push(entry);
        user.touch(entry.kind, entry.id);
        for (const kind of shown[entry.kind]) kinds.add(kind);
      }
      if (kinds.size > 0) notify({ userId, kinds: [...kinds], cause: undefined });
    }

    const withdraw = (entries: Iterable<Entry>) => {
      const gone = new Set<CacheKind>();
      for (const entry of [...entries]) remove(entry, gone);
      if (gone.size > 0) notify({ userId, kinds: [...gone], cause: undefined });
    };

    let result: T;
    try {
      result = await call(batch.nonce);
    } catch (error) {
      withdraw(batch.entries);
      throw error;
    }
    batch.answered = true;
    const only = created.length === 1 ? created[0]! : undefined;
    const answeredId = (result as { id?: unknown } | null | undefined)?.id;
    if (only && batch.entries.has(only) && typeof answeredId === "string") {
      answered.set(only.id, answeredId);
    }
    const current = users.get(userId);
    withdraw([...batch.entries].filter((entry) => !current || arrived(current, entry)));
    if (batch.entries.size > 0) {
      batch.timer = setTimeout(() => withdraw(batch.entries), settleAfter);
    }
    return result;
  }

  return {
    user: (userId) => userOf(userId),
    users: () => {
      const base = cache.users();
      if (list?.base !== base) {
        list = { base, users: Object.freeze(base.map((user) => userOf(user.userId)!)) };
      }
      return list.users;
    },
    subscribe: (listener) => {
      const own = (change: CacheChange) => listener(change);
      listeners.add(own);
      return () => {
        listeners.delete(own);
      };
    },
    apply,
    tempId: () => `tmp_${randomText(16)}`,
    resolveId: (tempId) => resolved.get(tempId),
  };
}
