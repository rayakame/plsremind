import type {
  Client,
  FollowUp,
  Item,
  ItemInput,
  List,
  ManualReminder,
  Priority,
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

/** The real id of an object, waiting for the hub's answer if it is still a draft. */
function settled(hub: Writer, id: string): Promise<string> {
  if (!isDraft(id)) return Promise.resolve(id);
  const known = hub.view.resolveId(id);
  if (known !== undefined) return Promise.resolve(known);
  return new Promise((resolve, reject) => {
    const stop = hub.view.subscribe(() => {
      const real = hub.view.resolveId(id);
      if (real === undefined) return;
      finish();
      resolve(real);
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

const chains = new WeakMap<OptimisticView, Map<string, Promise<unknown>>>();
const parts = new WeakMap<OptimisticView, Map<string, string>>();

/** Runs the calls about one item's parts one after the other, in the order they were made. */
function inOrder<T>(hub: Writer, itemId: string, work: () => Promise<T>): Promise<T> {
  let byItem = chains.get(hub.view);
  if (!byItem) chains.set(hub.view, (byItem = new Map()));
  const next = (byItem.get(itemId) ?? Promise.resolve()).then(work, work);
  byItem.set(
    itemId,
    next.catch(() => {}),
  );
  return next;
}

/** The real id of a subitem or reminder that was created in this session, or the id itself. */
function partId(hub: Writer, id: string): string {
  return parts.get(hub.view)?.get(id) ?? id;
}

function learnPart(hub: Writer, draft: string, real: string): void {
  let known = parts.get(hub.view);
  if (!known) parts.set(hub.view, (known = new Map()));
  known.set(draft, real);
}

function zoneOf(hub: Writer): string {
  const profile = hub.view.user(hub.userId)?.profile;
  return profile?.timezone ?? "UTC";
}

/** A due time as the item shows it and as the hub wants it. */
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
  const created = await hub.view.apply(
    [{ kind: "item", create: item }],
    async (_nonce, nonceFor) =>
      hub.client.rest.createItem(
        { ...input, listId: await settled(hub, listId) },
        { nonce: nonceFor(id) },
      ),
    { user: hub.userId },
  );
  return created.id;
}

function updateItem(
  hub: Writer,
  item: Item,
  shown: (before: Item) => Item,
  call: (id: string, nonce: string) => Promise<unknown>,
): Promise<void> {
  return hub.view
    .apply(
      [{ kind: "item", id: item.id, update: shown }],
      async (nonce) => call(await settled(hub, item.id), nonce),
      { user: hub.userId },
    )
    .then(() => undefined);
}

export function setDone(hub: Writer, item: Item, done: boolean): Promise<void> {
  const status = done ? "done" : "open";
  return updateItem(
    hub,
    item,
    (before) => ({ ...before, status, completedAt: done ? new Date() : null }),
    (id, nonce) => hub.client.rest.updateItem(id, { status }, { nonce }),
  );
}

export function patchItem(hub: Writer, item: Item, patch: ItemPatch): Promise<void> {
  const { due, ...plain } = patch;
  const fields = due === undefined ? undefined : dueFields(due, zoneOf(hub));
  return updateItem(
    hub,
    item,
    (before) => ({ ...before, ...plain, ...fields?.shown }),
    async (id, nonce) =>
      hub.client.rest.updateItem(
        id,
        {
          ...fields?.sent,
          ...plain,
          ...(plain.listId ? { listId: await settled(hub, plain.listId) } : {}),
        },
        { nonce },
      ),
  );
}

export function removeItem(hub: Writer, item: Item): Promise<void> {
  return hub.view.apply(
    [{ kind: "item", id: item.id, remove: true }],
    async (nonce) => hub.client.rest.deleteItem(await settled(hub, item.id), { nonce }),
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
    (id, nonce) =>
      inOrder(hub, item.id, async () => {
        const created = await hub.client.rest.createSubitem(id, { title }, { nonce });
        learnPart(hub, draft.id, created.id);
      }),
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
    (id, nonce) =>
      inOrder(hub, item.id, () =>
        hub.client.rest.updateSubitem(id, partId(hub, subitem.id), patch, { nonce }),
      ),
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
    (id, nonce) =>
      inOrder(hub, item.id, () =>
        hub.client.rest.deleteSubitem(id, partId(hub, subitem.id), { nonce }),
      ),
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
      subitems: order.flatMap((id, position) => {
        const one = before.subitems.find(
          (candidate) => candidate.id === id || candidate.id === partId(hub, id),
        );
        return one ? [{ ...one, position }] : [];
      }),
    }),
    (id, nonce) =>
      inOrder(hub, item.id, () =>
        hub.client.rest.reorderSubitems(
          id,
          { ids: order.map((one) => partId(hub, one)) },
          { nonce },
        ),
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
    (id, nonce) =>
      inOrder(hub, item.id, async () => {
        const created = await hub.client.rest.createReminder(id, { at }, { nonce });
        learnPart(hub, draft.id, created.id);
      }),
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
    (id, nonce) =>
      inOrder(hub, item.id, () =>
        hub.client.rest.deleteReminder(id, partId(hub, reminder.id), { nonce }),
      ),
  );
}

/** Changes the profile: name, zone, language or quiet hours. */
export function updateProfile(hub: Writer, changes: UserChanges): Promise<void> {
  return hub.view
    .apply(
      [{ kind: "profile", update: (before) => ({ ...before, ...changes }) }],
      (nonce) => hub.client.rest.updateUser(changes, { nonce }),
      { user: hub.userId },
    )
    .then(() => undefined);
}

export function setPlan(
  hub: Writer,
  priority: Priority,
  plan: { before: readonly number[]; followUp: FollowUp },
): Promise<void> {
  return hub.view
    .apply(
      [
        {
          kind: "plan",
          priority,
          update: (before) => ({ ...before, ...plan, isDefault: false }),
        },
      ],
      (nonce) => hub.client.rest.setReminderPlan(priority, plan, { nonce }),
      { user: hub.userId },
    )
    .then(() => undefined);
}

/** Changes the dashboard's own preferences and keeps what other clients stored. */
export function setPrefs(hub: Writer, patch: Partial<Prefs>): Promise<void> {
  const profile = hub.view.user(hub.userId)?.profile as User | undefined;
  const preferences = mergePrefs(profile?.preferences, patch);
  return hub.view
    .apply(
      [{ kind: "profile", update: (before) => ({ ...before, preferences }) }],
      (nonce) => hub.client.rest.updateUser({ preferences }, { nonce }),
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
  const created = await hub.view.apply(
    [{ kind: "list", create: list }],
    (_nonce, nonceFor) => hub.client.rest.createList({ name }, { nonce: nonceFor(id) }),
    { user: hub.userId },
  );
  return created.id;
}

export function renameList(hub: Writer, list: List, name: string): Promise<void> {
  return hub.view
    .apply(
      [{ kind: "list", id: list.id, update: (before) => ({ ...before, name }) }],
      async (nonce) => hub.client.rest.renameList(await settled(hub, list.id), { name }, { nonce }),
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
    async (nonce) => hub.client.rest.deleteList(await settled(hub, list.id), { nonce }),
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
      async (nonce) =>
        hub.client.rest.reorderLists(
          { ids: await Promise.all(order.map((id) => settled(hub, id))) },
          { nonce },
        ),
      { user: hub.userId },
    )
    .then(() => undefined);
}
