import type {
  Delivery,
  DeliveryStatus,
  DeliveryTest,
  Intent,
  Item,
  List,
  ManualReminder,
  Profile,
  ReminderPlan,
  Subitem,
  UserConnection,
} from "./types.js";

/** Who made a change. Compare `id` with `client.principal.id` to recognise your own. */
export interface Actor {
  readonly kind: "session" | "personal_token" | "connection";
  readonly id: string;
}

/** Who the client is logged in as. `id` is the token, login or connection it came with. */
export type Principal =
  | {
      readonly kind: "personal_token" | "session";
      readonly id: string;
      readonly userId: string;
    }
  | {
      readonly kind: "connection";
      readonly id: string;
      readonly connection: {
        readonly id: string;
        readonly name: string;
        readonly provider: string;
      };
    };

export interface ChangeEvent<D, O = D> {
  /** The user the change is about. */
  readonly userId: string;
  /** Null when the hub made the change by itself. */
  readonly actor: Actor | null;
  /** The nonce of the request that made the change, if it sent one. */
  readonly nonce: string | null;
  /** The whole object after the change. */
  readonly data: D;
  /** The object as the cache held it before, if it held one. */
  readonly old: O | undefined;
}

export type UserUpdatedEvent = ChangeEvent<Profile>;
export type ConnectionUpdatedEvent = ChangeEvent<UserConnection, never>;
export type ListCreatedEvent = ChangeEvent<List>;
export type ListUpdatedEvent = ChangeEvent<List>;
export type ListDeletedEvent = ChangeEvent<{ readonly id: string }, List>;
export type ItemCreatedEvent = ChangeEvent<Item>;
export type ItemUpdatedEvent = ChangeEvent<Item>;
export type ItemDeletedEvent = ChangeEvent<{ readonly id: string }, Item>;
export type SubitemCreatedEvent = ChangeEvent<Subitem>;
export type SubitemUpdatedEvent = ChangeEvent<Subitem>;
export type SubitemDeletedEvent = ChangeEvent<
  { readonly id: string; readonly itemId: string },
  Subitem
>;
export type ReminderCreatedEvent = ChangeEvent<ManualReminder>;
export type ReminderDeletedEvent = ChangeEvent<
  { readonly id: string; readonly itemId: string },
  ManualReminder
>;
export type PlanUpdatedEvent = ChangeEvent<ReminderPlan>;

export interface DeliveryCreatedEvent {
  readonly userId: string;
  readonly actor: null;
  readonly nonce: null;
  readonly data: Delivery;
}

export interface DeliveryTestEvent {
  readonly userId: string;
  readonly actor: null;
  readonly nonce: null;
  readonly data: DeliveryTest;
}

export interface DeliveryClosedEvent {
  readonly userId: string;
  readonly actor: null;
  readonly nonce: null;
  readonly data: { readonly id: string; readonly status: DeliveryStatus };
}

/** One part of a user's state as it arrives. Most code wants `user.ready` instead. */
export interface UserSyncEvent {
  readonly userId: string;
  readonly chunk: { readonly index: number; readonly count: number };
  readonly user?: Profile;
  readonly lists?: readonly List[];
  readonly items?: readonly Item[];
  readonly plans?: readonly ReminderPlan[];
  readonly history?: readonly Item[];
  readonly historyCursor?: string | null;
}

/** A connection lost a user, or a new session no longer covers them. */
export interface UserGoneEvent {
  readonly userId: string;
}

export interface ReadyEvent {
  readonly principal: Principal;
  readonly users: readonly string[];
  readonly intents: readonly Intent[];
}

export interface UserReadyEvent {
  readonly userId: string;
}

export type ResumedEvent = Readonly<Record<string, never>>;

export interface DisconnectedEvent {
  readonly code: number | undefined;
  readonly reason: string;
}

export interface ClosedEvent {
  readonly code: number | undefined;
  readonly reason: string;
}

export interface ErrorEvent {
  readonly error: unknown;
  /** The event that was being handled. */
  readonly event: string;
}

export interface EventMap {
  "user.sync": UserSyncEvent;
  "user.gone": UserGoneEvent;
  "user.updated": UserUpdatedEvent;
  "connection.updated": ConnectionUpdatedEvent;
  "list.created": ListCreatedEvent;
  "list.updated": ListUpdatedEvent;
  "list.deleted": ListDeletedEvent;
  "item.created": ItemCreatedEvent;
  "item.updated": ItemUpdatedEvent;
  "item.deleted": ItemDeletedEvent;
  "subitem.created": SubitemCreatedEvent;
  "subitem.updated": SubitemUpdatedEvent;
  "subitem.deleted": SubitemDeletedEvent;
  "reminder.created": ReminderCreatedEvent;
  "reminder.deleted": ReminderDeletedEvent;
  "plan.updated": PlanUpdatedEvent;
  "delivery.created": DeliveryCreatedEvent;
  "delivery.closed": DeliveryClosedEvent;
  /** The user asked to see that this app reaches them; answer with `rest.reportTest`. */
  "delivery.test": DeliveryTestEvent;
  /** After every new login, once the state of every user is complete. */
  ready: ReadyEvent;
  /** One user's state is complete. */
  "user.ready": UserReadyEvent;
  /** A dropped connection was picked up again without losing anything. */
  resumed: ResumedEvent;
  /** The connection is gone and is being opened again. */
  disconnected: DisconnectedEvent;
  /** The client gave up: the first connect failed, or the hub refuses the token or intents. */
  closed: ClosedEvent;
  /** A handler threw or its promise rejected, or an event could not be taken in. */
  error: ErrorEvent;
}

export const hubEventNames = [
  "user.sync",
  "user.gone",
  "user.updated",
  "connection.updated",
  "list.created",
  "list.updated",
  "list.deleted",
  "item.created",
  "item.updated",
  "item.deleted",
  "subitem.created",
  "subitem.updated",
  "subitem.deleted",
  "reminder.created",
  "reminder.deleted",
  "plan.updated",
  "delivery.created",
  "delivery.closed",
  "delivery.test",
] as const satisfies readonly (keyof EventMap)[];
