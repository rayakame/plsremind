import { ApiError, NetworkError } from "plsremind";
import type {
  Client,
  FollowUp,
  Item,
  ItemInput,
  List,
  ManualReminder,
  Priority,
  QuietHours,
  ReminderPlan,
  ReminderOverride,
  Subitem,
  User,
  UserChanges,
} from "plsremind";
import type { OptimisticView } from "plsremind/optimistic";
import type { When } from "../time/when.js";
import { mergePrefs, type Prefs } from "./prefs.js";
import { instantAt } from "../time/zone.js";

/** What a write needs: the client to call and the view to show the change in. */
export interface Writer {
  client: Client;
  view: OptimisticView;
  userId: string;
}

export interface ItemDraft {
  title: string;
  listId?: string | undefined;
  note?: string | undefined;
  priority?: Priority | undefined;
  due?: When | null | undefined;
}

export interface ItemPatch {
  title?: string;
  note?: string;
  priority?: Priority;
  listId?: string;
  due?: When | null;
  reminderOverride?: ReminderOverride | null;
}

const isDraft = (id: string) => id.startsWith("tmp_");
const waitForDraft = 15_000;
const waitForAnswer = 30_000;

function settled(hub: Writer, id: string): Promise<string> {
  if (!isDraft(id)) return Promise.resolve(id);
  const known = hub.view.resolveId(id);
  if (known !== undefined) return Promise.resolve(known);
  return new Promise((resolve, reject) => {
    const stop = hub.view.subscribe(() => {
      const real = hub.view.resolveId(id);
      if (real !== undefined) {
        finish();
        return resolve(real);
      }
    });
    const timer = setTimeout(() => {
      finish();
      reject(new Error("The object was never created."));
    }, waitForDraft);
    function finish() {
      stop();
      clearTimeout(timer);
    }
  });
}

interface Memory {
  /** The last call about each object, by the id it was first known under. */
  chains: Map<string, Promise<unknown>>;
  /** Drafts of items and lists made here, to find the chain of the object they became. */
  drafts: Set<string>;
  /** Subitems and reminders made here: draft id to real id and back. */
  parts: Map<string, string>;
  partDrafts: Map<string, string>;
  /** How many calls about each object have come back, and who wants to know. */
  answered: Map<string, number>;
  listeners: Set<() => void>;
  /** Nonces of calls whose event the gateway has not brought yet, and of events that came first. */
  issued: Set<string>;
  seen: Set<string>;
  /** What the hub answered to those calls, by what they were about. */
  unseen: Map<string, { nonce: string; value: unknown; at: number }[]>;
  /** How many times the gateway has sent the whole state anew. */
  syncs: number;
  /** What neither the cache nor an answer can be trusted on until the hub was asked. */
  doubt: Set<string>;
  /** Changes to items the view does not hold, for the page that shows such an item. */
  ahead: Map<string, ((item: Item) => Item)[]>;
  /** Those of them the hub has answered; they go once the item was read again. */
  aheadAnswered: Set<(item: Item) => Item>;
  aheadAnswers: Map<string, number>;
  aheadVersion: number;
}

const memories = new WeakMap<OptimisticView, Memory>();

function memoryOf(hub: Writer): Memory {
  const known = memories.get(hub.view);
  if (known) return known;
  const memory: Memory = {
    chains: new Map(),
    drafts: new Set(),
    parts: new Map(),
    partDrafts: new Map(),
    answered: new Map(),
    listeners: new Set(),
    issued: new Set(),
    seen: new Set(),
    unseen: new Map(),
    syncs: 0,
    doubt: new Set(),
    ahead: new Map(),
    aheadAnswered: new Set(),
    aheadAnswers: new Map(),
    aheadVersion: 0,
  };
  const { issued, seen, unseen } = memory;
  hub.client.cache.subscribe((change) => {
    if (change.userId !== hub.userId) return;
    if (!change.cause) {
      memory.syncs++;
      // The sync may have been read before an answered change was stored.
      for (const [key, answers] of unseen) if (answers.length > 0) memory.doubt.add(key);
      return unseen.clear();
    }
    const nonce = change.cause.nonce;
    if (!nonce || !issued.delete(nonce)) return;
    seen.add(nonce);
    for (const [key, answers] of unseen) {
      unseen.set(
        key,
        answers.filter((answer) => answer.nonce !== nonce),
      );
    }
  });
  memories.set(hub.view, memory);
  return memory;
}

// An event follows its answer within moments. After this long the hub is asked instead.
const trustAnswer = 5000;

/**
 * Marks a call whose answer is to be kept until the gateway has brought the event for it.
 * Until then the cache is behind, and a write that sends a whole object would be built on
 * the older state. Returns the function to hand the answer to.
 */
function issue(hub: Writer, nonce: string): (key: string, value: unknown) => void {
  const memory = memoryOf(hub);
  const syncs = memory.syncs;
  memory.issued.add(nonce);
  return (key, value) => {
    if (memory.seen.delete(nonce)) return;
    // After a sync in between nobody can tell whether it already held this change.
    if (memory.syncs !== syncs) {
      memory.issued.delete(nonce);
      memory.doubt.add(key);
      return;
    }
    memory.unseen.set(key, [...(memory.unseen.get(key) ?? []), { nonce, value, at: Date.now() }]);
  };
}

function recalled<T>(hub: Writer, key: string): T[] {
  return (memoryOf(hub).unseen.get(key) ?? []).map((answer) => answer.value as T);
}

/**
 * What the hub holds, asked for when neither the cache nor a kept answer can be trusted.
 * Undefined if they can. The question stays open until a write built on the answer went through.
 */
async function asked<T>(hub: Writer, key: string, ask: () => Promise<T>): Promise<T | undefined> {
  const memory = memoryOf(hub);
  const stale = Date.now() - trustAnswer;
  const overdue = (memory.unseen.get(key) ?? []).some((answer) => answer.at <= stale);
  if (!overdue && !memory.doubt.has(key)) return undefined;
  memory.doubt.add(key);
  return ask();
}

/** The write built on the hub's own answer went through, so what was kept before is moot. */
function settledOn(hub: Writer, key: string, fromHub: unknown): void {
  if (fromHub === undefined) return;
  const memory = memoryOf(hub);
  memory.unseen.delete(key);
  memory.doubt.delete(key);
}

// A call without an answer would hold back every later change to the same object for good.
function limited<T>(work: (signal: AbortSignal) => Promise<T>): Promise<T> {
  const abort = new AbortController();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new NetworkError("The hub did not answer in time."));
      abort.abort();
    }, waitForAnswer);
    work(abort.signal)
      .then(resolve, reject)
      .finally(() => clearTimeout(timer));
  });
}

function chainKey(hub: Writer, id: string): string {
  if (isDraft(id)) return id;
  for (const draft of memoryOf(hub).drafts) {
    if (hub.view.resolveId(draft) === id) return draft;
  }
  return id;
}

/** A key for an item or list that stays the same when a draft made here becomes real. */
export function stableKey(hub: Writer, id: string): string {
  return chainKey(hub, id);
}

// Requests are not ordered on the wire, so the calls about one object go out one by one.
function inOrder<T>(
  hub: Writer,
  id: string,
  work: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  const memory = memoryOf(hub);
  const key = chainKey(hub, id);
  const run = () => limited(work);
  const next = (memory.chains.get(key) ?? Promise.resolve()).then(run, run);
  const after = () => {
    memory.answered.set(key, (memory.answered.get(key) ?? 0) + 1);
    for (const listener of [...memory.listeners]) listener();
  };
  memory.chains.set(key, next.then(after, after));
  return next;
}

/** How many calls about an object the hub has answered, to tell when to look at it again. */
export function answeredCalls(hub: Writer, id: string): number {
  return memoryOf(hub).answered.get(chainKey(hub, id)) ?? 0;
}

export function onAnsweredCall(hub: Writer, listener: () => void): () => void {
  const { listeners } = memoryOf(hub);
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function partId(hub: Writer, id: string): string {
  return memoryOf(hub).parts.get(id) ?? id;
}

function learnPart(hub: Writer, draft: string, real: string): void {
  const memory = memoryOf(hub);
  memory.parts.set(draft, real);
  memory.partDrafts.set(real, draft);
}

/** A key for a subitem or reminder that stays the same when a draft becomes real. */
export function partKey(hub: Writer, id: string): string {
  return memoryOf(hub).partDrafts.get(id) ?? id;
}

function zoneOf(hub: Writer): string {
  const profile = hub.view.user(hub.userId)?.profile;
  return profile?.timezone ?? "UTC";
}

function dueFields(due: When | null, zone: string) {
  if (!due) {
    return {
      shown: { dueAt: null, dueDate: null, allDay: false },
      sent: { dueAt: null } satisfies ItemInput,
    };
  }
  if (due.time === null) {
    const end = new Date(instantAt(due.day, "23:59", zone).getTime() + 59_000);
    return {
      shown: { dueAt: end, dueDate: due.day, allDay: true },
      sent: { dueDate: due.day } satisfies ItemInput,
    };
  }
  const at = instantAt(due.day, due.time, zone);
  return {
    shown: { dueAt: at, dueDate: null, allDay: false },
    sent: { dueAt: at } satisfies ItemInput,
  };
}

/** Creates an item, shown at once as a draft. Resolves with its real id. */
export async function createItem(hub: Writer, draft: ItemDraft): Promise<string> {
  const state = hub.view.user(hub.userId);
  const listId = draft.listId ?? state?.lists().find((list) => list.isInbox)?.id ?? "";
  const due = draft.due ? dueFields(draft.due, zoneOf(hub)) : undefined;
  const now = new Date();
  const id = hub.view.tempId();
  const item: Item = {
    id,
    listId,
    title: draft.title,
    note: draft.note ?? "",
    priority: draft.priority ?? "normal",
    dueAt: null,
    dueDate: null,
    allDay: false,
    ...due?.shown,
    status: "open",
    completedAt: null,
    reminderOverride: null,
    subitems: [],
    reminders: [],
    createdAt: now,
    updatedAt: now,
  };
  const input = {
    title: draft.title,
    ...(draft.note ? { note: draft.note } : {}),
    ...(draft.priority ? { priority: draft.priority } : {}),
    ...due?.sent,
  };
  memoryOf(hub).drafts.add(id);
  const created = await hub.view.apply(
    [{ kind: "item", create: item }],
    async (_nonce, nonceFor) =>
      limited(async (signal) =>
        hub.client.rest.createItem(
          { ...input, listId: await settled(hub, listId) },
          { nonce: nonceFor(id), signal },
        ),
      ),
    { user: hub.userId },
  );
  return created.id;
}

function updateItem(
  hub: Writer,
  item: Item,
  shown: (before: Item) => Item,
  call: (id: string, nonce: string, signal: AbortSignal) => Promise<unknown>,
): Promise<void> {
  // The view only shows changes to what it holds. An item fetched by its page is not among it.
  const outside = hub.view.user(hub.userId)?.item(item.id) === undefined;
  if (outside) setAhead(hub, item.id, (before) => [...before, shown]);
  return hub.view
    .apply(
      [{ kind: "item", id: item.id, update: shown }],
      (nonce) =>
        inOrder(hub, item.id, async (signal) => call(await settled(hub, item.id), nonce, signal)),
      { user: hub.userId },
    )
    .then(
      () => {
        if (!outside) return;
        const memory = memoryOf(hub);
        memory.aheadAnswered.add(shown);
        memory.aheadAnswers.set(item.id, (memory.aheadAnswers.get(item.id) ?? 0) + 1);
        setAhead(hub, item.id, (before) => before);
      },
      (error: unknown) => {
        if (outside) setAhead(hub, item.id, (before) => before.filter((one) => one !== shown));
        throw error;
      },
    );
}

type Ahead = ((item: Item) => Item)[];

function setAhead(hub: Writer, id: string, change: (before: Ahead) => Ahead): void {
  const memory = memoryOf(hub);
  memory.ahead.set(id, change(memory.ahead.get(id) ?? []));
  memory.aheadVersion++;
  for (const listener of [...memory.listeners]) listener();
}

/** An item the view does not hold, with the changes made to it here laid over it. */
export function aheadOf(hub: Writer, item: Item): Item {
  return (memoryOf(hub).ahead.get(item.id) ?? []).reduce((shown, change) => change(shown), item);
}

/** The changes to such an item that the hub has answered, so that a new read holds them. */
export function answeredAhead(hub: Writer, id: string): Ahead {
  const memory = memoryOf(hub);
  return (memory.ahead.get(id) ?? []).filter((one) => memory.aheadAnswered.has(one));
}

/** How many changes to such an item the hub has answered so far. */
export function aheadAnswers(hub: Writer, id: string): number {
  return memoryOf(hub).aheadAnswers.get(id) ?? 0;
}

/** Takes changes off an item once it was read again after they were answered. */
export function dropAhead(hub: Writer, id: string, changes: Ahead): void {
  if (changes.length === 0) return;
  for (const one of changes) memoryOf(hub).aheadAnswered.delete(one);
  setAhead(hub, id, (before) => before.filter((one) => !changes.includes(one)));
}

export function aheadVersion(hub: Writer): number {
  return memoryOf(hub).aheadVersion;
}

export function setDone(hub: Writer, item: Item, done: boolean): Promise<void> {
  const status = done ? "done" : "open";
  const completedAt = done ? new Date() : null;
  return updateItem(
    hub,
    item,
    (before) => ({ ...before, status, completedAt }),
    (id, nonce, signal) => hub.client.rest.updateItem(id, { status }, { nonce, signal }),
  );
}

export function patchItem(hub: Writer, item: Item, patch: ItemPatch): Promise<void> {
  const { due, ...plain } = patch;
  const fields = due === undefined ? undefined : dueFields(due, zoneOf(hub));
  return updateItem(
    hub,
    item,
    (before) => ({
      ...before,
      ...plain,
      ...(plain.listId ? { listId: hub.view.resolveId(plain.listId) ?? plain.listId } : {}),
      ...fields?.shown,
    }),
    async (id, nonce, signal) =>
      hub.client.rest.updateItem(
        id,
        {
          ...fields?.sent,
          ...plain,
          ...(plain.listId ? { listId: await settled(hub, plain.listId) } : {}),
        },
        { nonce, signal },
      ),
  );
}

export function removeItem(hub: Writer, item: Item): Promise<void> {
  return hub.view.apply(
    [{ kind: "item", id: item.id, remove: true }],
    (nonce) =>
      inOrder(hub, item.id, async (signal) =>
        hub.client.rest.deleteItem(await settled(hub, item.id), { nonce, signal }),
      ),
    { user: hub.userId },
  );
}

export function addSubitem(hub: Writer, item: Item, title: string): Promise<void> {
  const now = new Date();
  const draft: Subitem = {
    id: hub.view.tempId(),
    itemId: item.id,
    title,
    done: false,
    position: item.subitems.length,
    createdAt: now,
    updatedAt: now,
  };
  return updateItem(
    hub,
    item,
    (before) =>
      before.subitems.some((one) => one.id === partId(hub, draft.id))
        ? before
        : { ...before, subitems: [...before.subitems, draft] },
    async (id, nonce, signal) => {
      const created = await hub.client.rest.createSubitem(id, { title }, { nonce, signal });
      learnPart(hub, draft.id, created.id);
    },
  );
}

export function patchSubitem(
  hub: Writer,
  item: Item,
  subitem: Subitem,
  patch: { title?: string; done?: boolean },
): Promise<void> {
  return updateItem(
    hub,
    item,
    (before) => ({
      ...before,
      subitems: before.subitems.map((one) =>
        one.id === subitem.id || one.id === partId(hub, subitem.id) ? { ...one, ...patch } : one,
      ),
    }),
    (id, nonce, signal) =>
      hub.client.rest.updateSubitem(id, partId(hub, subitem.id), patch, { nonce, signal }),
  );
}

export function removeSubitem(hub: Writer, item: Item, subitem: Subitem): Promise<void> {
  return updateItem(
    hub,
    item,
    (before) => ({
      ...before,
      subitems: before.subitems.filter(
        (one) => one.id !== subitem.id && one.id !== partId(hub, subitem.id),
      ),
    }),
    (id, nonce, signal) =>
      hub.client.rest.deleteSubitem(id, partId(hub, subitem.id), { nonce, signal }),
  );
}

export function moveSubitem(hub: Writer, item: Item, subitem: Subitem, by: -1 | 1): Promise<void> {
  const order = item.subitems.map((one) => one.id);
  const from = order.indexOf(subitem.id);
  const to = from + by;
  if (from < 0 || to < 0 || to >= order.length) return Promise.resolve();
  [order[from], order[to]] = [order[to]!, order[from]!];
  return updateItem(
    hub,
    item,
    (before) => ({
      ...before,
      subitems: [
        ...order.flatMap((id, position) => {
          const one = before.subitems.find(
            (candidate) => candidate.id === id || candidate.id === partId(hub, id),
          );
          return one ? [{ ...one, position }] : [];
        }),
        ...before.subitems.filter(
          (one) => !order.some((id) => one.id === id || one.id === partId(hub, id)),
        ),
      ],
    }),
    (id, nonce, signal) =>
      hub.client.rest.reorderSubitems(
        id,
        { ids: order.map((one) => partId(hub, one)) },
        { nonce, signal },
      ),
  );
}

export function addReminder(hub: Writer, item: Item, at: Date): Promise<void> {
  const draft: ManualReminder = {
    id: hub.view.tempId(),
    itemId: item.id,
    remindAt: at,
    createdAt: new Date(),
  };
  return updateItem(
    hub,
    item,
    (before) =>
      before.reminders.some((one) => one.id === partId(hub, draft.id))
        ? before
        : {
            ...before,
            reminders: [...before.reminders, draft].sort(
              (a, b) => a.remindAt.getTime() - b.remindAt.getTime(),
            ),
          },
    async (id, nonce, signal) => {
      const created = await hub.client.rest.createReminder(id, { at }, { nonce, signal });
      learnPart(hub, draft.id, created.id);
    },
  );
}

export function removeReminder(hub: Writer, item: Item, reminder: ManualReminder): Promise<void> {
  return updateItem(
    hub,
    item,
    (before) => ({
      ...before,
      reminders: before.reminders.filter(
        (one) => one.id !== reminder.id && one.id !== partId(hub, reminder.id),
      ),
    }),
    (id, nonce, signal) =>
      hub.client.rest
        .deleteReminder(id, partId(hub, reminder.id), { nonce, signal })
        .catch((error: unknown) => {
          // The hub drops a reminder by itself once its time has passed.
          if (!(error instanceof ApiError && error.status === 404)) throw error;
        }),
  );
}

/** Changes the profile: name, zone, language or quiet hours. */
export function updateProfile(hub: Writer, changes: UserChanges): Promise<void> {
  return hub.view
    .apply(
      [{ kind: "profile", update: (before) => ({ ...before, ...changes }) }],
      (nonce) =>
        inOrder(hub, "profile", (signal) => hub.client.rest.updateUser(changes, { nonce, signal })),
      { user: hub.userId },
    )
    .then(() => undefined);
}

/** Changes part of a plan. The rest is sent as the hub last had it, not as it is shown. */
export function setPlan(
  hub: Writer,
  priority: Priority,
  patch: { before?: readonly number[]; followUp?: FollowUp },
): Promise<void> {
  const key = `plan:${priority}`;
  return hub.view
    .apply(
      [{ kind: "plan", priority, update: (before) => ({ ...before, ...patch, isDefault: false }) }],
      (nonce) =>
        inOrder(hub, key, async (signal) => {
          const fromHub = await asked(hub, key, async () =>
            (await hub.client.rest.listReminderPlans({ signal })).find(
              (plan) => plan.priority === priority,
            ),
          );
          const held =
            fromHub ??
            recalled<ReminderPlan>(hub, key).at(-1) ??
            hub.client.cache.user(hub.userId)?.plan(priority);
          const answered = issue(hub, nonce);
          const answer = await hub.client.rest.setReminderPlan(
            priority,
            {
              before: patch.before ?? held?.before ?? [],
              followUp: patch.followUp ?? held?.followUp ?? "never",
            },
            { nonce, signal },
          );
          settledOn(hub, key, fromHub);
          answered(key, answer);
        }),
      { user: hub.userId },
    )
    .then(() => undefined);
}

export function resetPlan(hub: Writer, priority: Priority): Promise<void> {
  const key = `plan:${priority}`;
  return inOrder(hub, key, async (signal) => {
    await hub.client.rest.resetReminderPlan(priority, { signal });
    memoryOf(hub).unseen.delete(key);
  });
}

const quietDefault: QuietHours = { start: "22:00", end: "07:00" };

/** Turns quiet hours on or off, or changes one end. The other end is sent as the hub last had it. */
export function setQuiet(hub: Writer, part: Partial<QuietHours> | null): Promise<void> {
  return hub.view
    .apply(
      [
        {
          kind: "profile",
          update: (before) => ({
            ...before,
            quietHours:
              part === null ? null : { ...((before as User).quietHours ?? quietDefault), ...part },
          }),
        },
      ],
      (nonce) =>
        inOrder(hub, "profile", async (signal) => {
          const fromHub = await asked(hub, "quiet", async () => ({
            quiet: ((await hub.client.rest.getUser({ signal })) as User).quietHours,
          }));
          const last = fromHub ?? recalled<{ quiet: QuietHours | null }>(hub, "quiet").at(-1);
          const confirmed = hub.client.cache.user(hub.userId)?.profile as User | undefined;
          const held = last ? last.quiet : (confirmed?.quietHours ?? null);
          const quietHours = part === null ? null : { ...(held ?? quietDefault), ...part };
          const answered = issue(hub, nonce);
          const answer = await hub.client.rest.updateUser({ quietHours }, { nonce, signal });
          settledOn(hub, "quiet", fromHub);
          answered("quiet", { quiet: answer.quietHours });
        }),
      { user: hub.userId },
    )
    .then(() => undefined);
}

/** Changes the dashboard's own preferences and keeps what other clients stored. */
export function setPrefs(hub: Writer, patch: Partial<Prefs>): Promise<void> {
  return hub.view
    .apply(
      [
        {
          kind: "profile",
          update: (before) => ({
            ...before,
            preferences: mergePrefs((before as User).preferences, patch),
          }),
        },
      ],
      (nonce) =>
        inOrder(hub, "profile", async (signal) => {
          // The hub replaces the whole object, so it is built from what the hub holds plus
          // our own changes it has answered, never from one that may yet be refused.
          const confirmed = (hub.client.cache.user(hub.userId)?.profile as User | undefined)
            ?.preferences;
          const fromHub = await asked(
            hub,
            "prefs",
            async () => ((await hub.client.rest.getUser({ signal })) as User).preferences,
          );
          const base =
            fromHub ?? recalled<Record<string, unknown>>(hub, "prefs").at(-1) ?? confirmed;
          const preferences = mergePrefs(base, patch);
          const answered = issue(hub, nonce);
          await hub.client.rest.updateUser({ preferences }, { nonce, signal });
          settledOn(hub, "prefs", fromHub);
          answered("prefs", preferences);
        }),
      { user: hub.userId },
    )
    .then(() => undefined);
}

/** Creates a list, shown at once as a draft. Resolves with its real id. */
export async function createList(hub: Writer, name: string): Promise<string> {
  const lists = hub.view.user(hub.userId)?.lists() ?? [];
  const now = new Date();
  const id = hub.view.tempId();
  const list: List = {
    id,
    name,
    position: Math.max(-1, ...lists.map((one) => one.position)) + 1,
    isInbox: false,
    createdAt: now,
    updatedAt: now,
  };
  memoryOf(hub).drafts.add(id);
  const created = await hub.view.apply(
    [{ kind: "list", create: list }],
    (_nonce, nonceFor) =>
      limited((signal) => hub.client.rest.createList({ name }, { nonce: nonceFor(id), signal })),
    { user: hub.userId },
  );
  return created.id;
}

export function renameList(hub: Writer, list: List, name: string): Promise<void> {
  return hub.view
    .apply(
      [{ kind: "list", id: list.id, update: (before) => ({ ...before, name }) }],
      (nonce) =>
        inOrder(hub, list.id, async (signal) =>
          hub.client.rest.renameList(await settled(hub, list.id), { name }, { nonce, signal }),
        ),
      { user: hub.userId },
    )
    .then(() => undefined);
}

/** Deletes a list. Its items move to the inbox, as the hub does it. */
export function removeList(hub: Writer, list: List): Promise<void> {
  const state = hub.view.user(hub.userId);
  const inbox = state?.lists().find((one) => one.isInbox)?.id;
  const moved = inbox
    ? [...(state?.items() ?? []), ...(state?.history() ?? [])].filter(
        (item) => item.listId === list.id,
      )
    : [];
  return hub.view.apply(
    [
      { kind: "list", id: list.id, remove: true },
      ...moved.map((item) => ({
        kind: "item" as const,
        id: item.id,
        update: (before: Item) => ({ ...before, listId: inbox! }),
      })),
    ],
    (nonce) =>
      inOrder(hub, list.id, async (signal) =>
        hub.client.rest.deleteList(await settled(hub, list.id), { nonce, signal }),
      ),
    { user: hub.userId },
  );
}

export function moveList(hub: Writer, list: List, by: -1 | 1): Promise<void> {
  const order = (hub.view.user(hub.userId)?.lists() ?? []).map((one) => one.id);
  const from = order.indexOf(list.id);
  const to = from + by;
  if (from < 0 || to < 0 || to >= order.length) return Promise.resolve();
  [order[from], order[to]] = [order[to]!, order[from]!];
  return hub.view
    .apply(
      order.map((id, position) => ({
        kind: "list" as const,
        id,
        update: (before: List) => ({ ...before, position }),
      })),
      (nonce) =>
        inOrder(hub, "lists", async (signal) =>
          hub.client.rest.reorderLists(
            { ids: await Promise.all(order.map((id) => settled(hub, id))) },
            { nonce, signal },
          ),
        ),
      { user: hub.userId },
    )
    .then(() => undefined);
}
