export type Priority = "low" | "normal" | "high";
export type ItemStatus = "open" | "done";
export type FollowUp = "never" | "daily" | "twice_daily";
export type ReminderKind = "before" | "due" | "follow_up" | "manual";
export type DeliveryStatus =
  "pending" | "delivered" | "failed" | "superseded" | "cancelled" | "expired";
export type Intent = "users" | "lists" | "items" | "plans" | "history" | "deliveries";
export type Role = "user" | "admin";
export type Registration = "open" | "invite";
export type ProviderType = "password" | "github" | "discord" | "oidc";

/** One page of a listing. `nextCursor` is null on the last page. */
export interface Page<T> {
  readonly data: readonly T[];
  readonly nextCursor: string | null;
}

export interface LoginMethod {
  readonly key: string;
  readonly type: ProviderType;
  readonly name: string;
}

export interface Info {
  readonly name: string;
  readonly version: string;
  readonly gatewayUrl: string;
  readonly setupRequired: boolean;
  readonly registration: Registration;
  readonly loginMethods: readonly LoginMethod[];
}

/** Local times of day as text, for example "22:00". */
export interface QuietHours {
  readonly start: string;
  readonly end: string;
}

export interface User {
  readonly id: string;
  readonly displayName: string;
  readonly role: Role;
  readonly timezone: string;
  readonly quietHours: QuietHours | null;
  readonly locale: string | null;
  /** The user's own client settings. The SDK hands them through as they are. */
  readonly preferences: Readonly<Record<string, unknown>>;
  readonly createdAt: Date;
}

export interface UserSummary {
  readonly id: string;
  readonly displayName: string;
  readonly timezone: string;
  readonly quietHours: QuietHours | null;
  readonly locale: string | null;
}

/** The profile a connection sees of a user in the gateway. */
export interface ConnectionUser extends UserSummary {
  readonly externalIds: readonly string[];
  readonly remind: boolean;
}

export type Profile = User | ConnectionUser;

export interface NewPasswordAccount {
  readonly username: string;
  readonly password: string;
  readonly displayName?: string;
  readonly timezone?: string;
  readonly locale?: string;
}

export interface Identity {
  readonly id: string;
  readonly provider: string;
  readonly externalId: string;
  readonly displayName: string;
  readonly createdAt: Date;
}

export interface Settings {
  readonly name: string;
  readonly registration: Registration;
}

export interface Invite {
  readonly id: string;
  /** Only present in the answer that created the invite. */
  readonly code?: string;
  readonly createdAt: Date;
  readonly expiresAt: Date;
  readonly usedAt: Date | null;
  readonly usedBy: string | null;
}

export interface AdminUser {
  readonly id: string;
  readonly displayName: string;
  readonly role: Role;
  readonly timezone: string;
  readonly quietHours: QuietHours | null;
  readonly locale: string | null;
  readonly createdAt: Date;
  readonly disabled: boolean;
}

export interface PersonalToken {
  readonly id: string;
  readonly name: string;
  readonly remind: boolean;
  /** Only present in the answer that created the token. */
  readonly token?: string;
  readonly createdAt: Date;
  readonly lastUsedAt: Date | null;
}

export interface List {
  readonly id: string;
  readonly name: string;
  readonly position: number;
  readonly isInbox: boolean;
  readonly createdAt: Date;
  readonly updatedAt: Date;
}

export interface ListName {
  readonly name: string;
}

export interface Order {
  readonly ids: readonly string[];
}

export interface ReminderPlan {
  readonly priority: Priority;
  /** How long before the deadline to remind, in milliseconds. */
  readonly before: readonly number[];
  readonly followUp: FollowUp;
  readonly isDefault: boolean;
}

export interface ReminderOverride {
  readonly mode: "off" | "custom";
  readonly before?: readonly number[];
}

export interface ItemInput {
  readonly title?: string;
  readonly note?: string;
  readonly listId?: string;
  readonly priority?: Priority;
  readonly dueAt?: Date | null;
  /** A whole day as text, for example "2026-11-13". */
  readonly dueDate?: string | null;
  readonly subitems?: readonly string[];
  readonly reminderOverride?: ReminderOverride | null;
}

export interface Subitem {
  readonly id: string;
  readonly itemId: string;
  readonly title: string;
  readonly done: boolean;
  readonly position: number;
  readonly createdAt: Date;
  readonly updatedAt: Date;
}

export interface ManualReminder {
  readonly id: string;
  readonly itemId: string;
  readonly remindAt: Date;
  readonly createdAt: Date;
}

export interface Item {
  readonly id: string;
  readonly listId: string;
  readonly title: string;
  readonly note: string;
  readonly priority: Priority;
  readonly dueAt: Date | null;
  /** Set for all-day items: the day as text, for example "2026-11-13". */
  readonly dueDate: string | null;
  readonly allDay: boolean;
  readonly status: ItemStatus;
  readonly completedAt: Date | null;
  readonly reminderOverride: ReminderOverride | null;
  readonly subitems: readonly Subitem[];
  readonly reminders: readonly ManualReminder[];
  readonly createdAt: Date;
  readonly updatedAt: Date;
}

export interface ScheduledReminder {
  readonly kind: ReminderKind;
  readonly offset: number | null;
  readonly reminderId: string | null;
  readonly scheduledFor: Date;
  readonly fireAt: Date;
}

export interface Connection {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  readonly provider: string;
  readonly enabled: boolean;
  readonly allowedIntents: readonly Intent[];
  /** Only present in the answer that created the connection or rotated its token. */
  readonly token?: string;
  readonly createdAt: Date;
  readonly lastUsedAt: Date | null;
}

export interface UserConnection {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  readonly provider: string;
  readonly linked: boolean;
  readonly enabled: boolean;
  readonly remind: boolean;
  readonly intents: readonly Intent[];
}

export interface LoginProvider {
  readonly key: string;
  readonly type: ProviderType;
  readonly name: string;
  readonly loginEnabled: boolean;
  readonly linkEnabled: boolean;
  readonly clientId: string;
  readonly hasClientSecret: boolean;
  readonly issuerUrl: string;
}

export interface DeliveryReminder {
  readonly id: string;
  readonly kind: ReminderKind;
  readonly offset?: number;
  readonly scheduledFor: Date;
}

export interface DeliveryUser {
  readonly id: string;
  readonly displayName: string;
  readonly timezone: string;
  readonly locale: string | null;
  readonly externalIds?: readonly string[];
}

export interface Delivery {
  readonly id: string;
  readonly attempt: number;
  readonly late: boolean;
  readonly reminder: DeliveryReminder;
  readonly item: Item;
  readonly user: DeliveryUser;
}

export interface DeliveryResult {
  readonly id: string;
  readonly status: DeliveryStatus;
  readonly reason: string;
}

export interface LookupResult {
  readonly id: string;
  readonly displayName: string;
  readonly timezone: string;
}

export interface ReminderLogDelivery {
  readonly id: string;
  readonly channel: {
    readonly kind: "connection" | "token";
    readonly id: string;
    readonly name: string;
  };
  readonly status: DeliveryStatus;
  readonly reason: string;
  readonly attempts: number;
}

export interface ReminderLogEntry {
  readonly id: string;
  readonly itemId: string;
  readonly kind: ReminderKind;
  readonly offset?: number;
  readonly scheduledFor: Date;
  readonly firedAt: Date;
  readonly outcome: "fired" | "missed" | "superseded";
  readonly deliveries: readonly ReminderLogDelivery[];
}
