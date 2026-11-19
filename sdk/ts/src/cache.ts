import { fromWire, microsOf } from "./convert.js";
import type { Intent, Item, List, Priority, Profile, ReminderPlan } from "./types.js";

export type CacheKind = "profile" | "lists" | "items" | "history" | "plans";

export interface CacheChange {
  readonly userId: string;
  /** What changed for this user. All five when the user was loaded anew or went away. */
  readonly kinds: readonly CacheKind[];
  /** The event behind the change, or undefined for a sync or a user going away. */
  readonly cause:
    | { readonly name: string; readonly nonce: string | null; readonly id: string | undefined }
    | undefined;
}

/** What the cache holds about one user. Reading is synchronous and never changes anything. */
export interface UserState {
  readonly userId: string;
  readonly profile: Profile | undefined;
  /** The lists in their order. The array stays the same until a list changes. */
  lists(): readonly List[];
  list(id: string): List | undefined;
  /** Every open item. The array stays the same until an open item changes. */
  items(): readonly Item[];
  /** An open item, or a completed one from the history. */
  item(id: string): Item | undefined;
  /** Completed items, most recently completed first. Needs the `history` intent. */
  history(): readonly Item[];
  /** Where `listItems` continues after the history, or null if nothing older exists. */
  readonly historyCursor: string | null;
  plans(): readonly ReminderPlan[];
  plan(priority: Priority): ReminderPlan | undefined;
}

export interface Cache {
  /** The state of a user. Without an id: the user a personal token or a session belongs to. */
  user(userId?: string): UserState | undefined;
  users(): readonly UserState[];
  /** Calls the listener after every change. Returns a function that unsubscribes. */
  subscribe(listener: (change: CacheChange) => void): () => void;
}

export interface Applied {
  /** The payload in the SDK's form. */
  event: unknown;
  change: CacheChange | undefined;
  old: unknown;
  /** The user whose state just became complete. */
  synced: string | undefined;
}

const allKinds: readonly CacheKind[] = ["profile", "lists", "items", "history", "plans"];

interface Done {
  readonly item: Item;
  readonly at: bigint;
  readonly key: string;
}

function newerFirst(a: Done, b: Done): number {
  if (a.at !== b.at) return a.at > b.at ? -1 : 1;
  if (a.key !== b.key) return a.key > b.key ? -1 : 1;
  return 0;
}

class Slots {
  profile: Profile | undefined;
  readonly lists = new Map<string, List>();
  readonly items = new Map<string, Item>();
  readonly done = new Map<string, Done>();
  readonly history: Done[] = [];
  historyCursor: string | null = null;
  readonly plans = new Map<Priority, ReminderPlan>();

  insertDone(item: Item, completedAt: string): void {
    let at = 0n;
    try {
      at = microsOf(completedAt);
    } catch {
      // An unreadable time sorts last instead of keeping the item out.
    }
    const entry: Done = { item, at, key: item.id.toLowerCase() };
    let low = 0;
    let high = this.history.length;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (newerFirst(this.history[middle]!, entry) < 0) low = middle + 1;
      else high = middle;
    }
    this.history.splice(low, 0, entry);
    this.done.set(item.id, entry);
  }

  removeDone(id: string): Item | undefined {
    const entry = this.done.get(id);
    if (!entry) return undefined;
    this.done.delete(id);
    this.history.splice(this.history.indexOf(entry), 1);
    return entry.item;
  }
}

class UserData implements UserState {
  slots = new Slots();
  #lists: readonly List[] | undefined;
  #items: readonly Item[] | undefined;
  #history: readonly Item[] | undefined;
  #plans: readonly ReminderPlan[] | undefined;

  constructor(readonly userId: string) {}

  touch(kinds: readonly CacheKind[]): void {
    for (const kind of kinds) {
      if (kind === "lists") this.#lists = undefined;
      else if (kind === "items") this.#items = undefined;
      else if (kind === "history") this.#history = undefined;
      else if (kind === "plans") this.#plans = undefined;
    }
  }

  get profile(): Profile | undefined {
    return this.slots.profile;
  }

  get historyCursor(): string | null {
    return this.slots.historyCursor;
  }

  lists(): readonly List[] {
    return (this.#lists ??= Object.freeze(
      [...this.slots.lists.values()].sort((a, b) => a.position - b.position),
    ));
  }

  list(id: string): List | undefined {
    return this.slots.lists.get(id);
  }

  items(): readonly Item[] {
    return (this.#items ??= Object.freeze([...this.slots.items.values()]));
  }

  item(id: string): Item | undefined {
    return this.slots.items.get(id) ?? this.slots.done.get(id)?.item;
  }

  history(): readonly Item[] {
    return (this.#history ??= Object.freeze(this.slots.history.map((entry) => entry.item)));
  }

  plans(): readonly ReminderPlan[] {
    return (this.#plans ??= Object.freeze([...this.slots.plans.values()]));
  }

  plan(priority: Priority): ReminderPlan | undefined {
    return this.slots.plans.get(priority);
  }
}

interface WireEvent {
  user_id: string;
  nonce?: string | null;
  data?: Record<string, unknown>;
}

interface WireSync {
  user_id: string;
  chunk: { index: number; count: number };
  user?: unknown;
  lists?: unknown[];
  items?: Record<string, unknown>[];
  plans?: unknown[];
  history?: Record<string, unknown>[];
  history_cursor?: string | null;
}

/** The writing side of the cache. Only gateway events go in. */
export class CacheStore {
  readonly #users = new Map<string, UserData>();
  readonly #pending = new Map<string, Slots>();
  readonly #listeners = new Set<(change: CacheChange) => void>();
  #list: readonly UserState[] | undefined;
  #self: string | undefined;
  #history = false;

  readonly cache: Cache = {
    user: (userId) => {
      const id = userId ?? this.#self;
      return id === undefined ? undefined : this.#users.get(id);
    },
    users: () => (this.#list ??= Object.freeze([...this.#users.values()])),
    subscribe: (listener) => {
      const own = (change: CacheChange) => listener(change);
      this.#listeners.add(own);
      return () => {
        this.#listeners.delete(own);
      };
    },
  };

  /** Starts a new session: users it no longer names go and are returned, the others stay readable. */
  begin(users: readonly string[], intents: readonly Intent[], self: string | undefined): string[] {
    this.#self = self;
    this.#history = intents.includes("history");
    this.#pending.clear();
    const named = new Set(users);
    const dropped = [...this.#users.keys()].filter((id) => !named.has(id));
    for (const id of dropped) this.#notify(this.#drop(id));
    return dropped;
  }

  apply(name: string, wire: unknown): Applied {
    const event = fromWire(wire);
    const applied: Applied = { event, change: undefined, old: undefined, synced: undefined };
    if (wire === null || typeof wire !== "object") return applied;
    const userId = (wire as WireEvent).user_id;
    if (name === "user.sync") {
      this.#sync(wire as WireSync, event as Synced, applied);
    } else if (name === "user.gone") {
      this.#pending.delete(userId);
      if (this.#users.has(userId)) applied.change = this.#drop(userId);
    } else {
      const user = this.#users.get(userId);
      const data = (wire as WireEvent).data;
      if (user && data !== null && typeof data === "object") {
        const kinds = this.#change(user, name, data, (event as { data: unknown }).data, applied);
        if (kinds.length > 0) {
          user.touch(kinds);
          const id = name === "plan.updated" ? data.priority : data.id;
          applied.change = {
            userId,
            kinds,
            cause: {
              name,
              nonce: (wire as WireEvent).nonce ?? null,
              id: typeof id === "string" ? id : undefined,
            },
          };
        }
      }
    }
    if (applied.change) this.#notify(applied.change);
    return applied;
  }

  #sync(wire: WireSync, event: Synced, applied: Applied): void {
    const userId = wire.user_id;
    let slots = this.#pending.get(userId);
    if (wire.chunk.index === 0) {
      slots = new Slots();
      this.#pending.set(userId, slots);
      slots.profile = event.user;
      for (const list of event.lists ?? []) slots.lists.set(list.id, list);
      for (const plan of event.plans ?? []) slots.plans.set(plan.priority, plan);
    }
    if (!slots) return;
    for (const item of event.items ?? []) slots.items.set(item.id, item);
    (event.history ?? []).forEach((item, i) => {
      slots.insertDone(item, wire.history![i]!.completed_at as string);
    });
    if (wire.history_cursor !== undefined) slots.historyCursor = wire.history_cursor;
    if (wire.chunk.index < wire.chunk.count - 1) return;

    this.#pending.delete(userId);
    let user = this.#users.get(userId);
    if (!user) {
      user = new UserData(userId);
      this.#users.set(userId, user);
      this.#list = undefined;
    }
    user.slots = slots;
    user.touch(allKinds);
    applied.synced = userId;
    applied.change = { userId, kinds: allKinds, cause: undefined };
  }

  #change(
    user: UserData,
    name: string,
    wire: Record<string, unknown>,
    data: unknown,
    applied: Applied,
  ): CacheKind[] {
    const slots = user.slots;
    const id = wire.id as string;
    switch (name) {
      case "user.updated":
        applied.old = slots.profile;
        slots.profile = data as Profile;
        return ["profile"];
      case "list.created":
      case "list.updated":
        applied.old = slots.lists.get(id);
        slots.lists.set(id, data as List);
        return ["lists"];
      case "list.deleted":
        applied.old = slots.lists.get(id);
        return slots.lists.delete(id) ? ["lists"] : [];
      case "plan.updated": {
        const plan = data as ReminderPlan;
        applied.old = slots.plans.get(plan.priority);
        slots.plans.set(plan.priority, plan);
        return ["plans"];
      }
      case "item.created":
      case "item.updated": {
        const item = data as Item;
        const open = slots.items.get(id);
        if (item.status === "open") {
          const done = slots.removeDone(id);
          applied.old = open ?? done;
          slots.items.set(id, item);
          return done ? ["items", "history"] : ["items"];
        }
        const kinds: CacheKind[] = [];
        if (open) {
          slots.items.delete(id);
          kinds.push("items");
        }
        if (this.#history && typeof wire.completed_at === "string") {
          applied.old = open ?? slots.removeDone(id);
          slots.insertDone(item, wire.completed_at);
          kinds.push("history");
        } else {
          applied.old = open;
        }
        return kinds;
      }
      case "item.deleted": {
        const open = slots.items.get(id);
        if (open) {
          applied.old = open;
          slots.items.delete(id);
          return ["items"];
        }
        applied.old = slots.removeDone(id);
        return applied.old ? ["history"] : [];
      }
      case "subitem.updated":
      case "subitem.deleted":
        applied.old = user.item(wire.item_id as string)?.subitems.find((sub) => sub.id === id);
        return [];
      case "reminder.deleted":
        applied.old = user.item(wire.item_id as string)?.reminders.find((rem) => rem.id === id);
        return [];
      default:
        return [];
    }
  }

  #drop(userId: string): CacheChange {
    this.#users.delete(userId);
    this.#list = undefined;
    return { userId, kinds: allKinds, cause: undefined };
  }

  #notify(change: CacheChange): void {
    for (const listener of [...this.#listeners]) {
      try {
        listener(change);
      } catch (error) {
        console.error("plsremind: a cache listener threw", error);
      }
    }
  }
}

interface Synced {
  user?: Profile;
  lists?: List[];
  items?: Item[];
  plans?: ReminderPlan[];
  history?: Item[];
}
