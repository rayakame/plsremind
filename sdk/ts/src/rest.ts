import { camelKeys, fromWire, toWire } from "./convert.js";
import { ApiError, NetworkError } from "./errors.js";
import type {
  AdminUser,
  Connection,
  ConnectionTest,
  Delivery,
  DeliveryResult,
  FollowUp,
  Identity,
  Info,
  Intent,
  Invite,
  Item,
  ItemInput,
  ItemStatus,
  List,
  ListName,
  LoginMethod,
  LoginProvider,
  LookupResult,
  ManualReminder,
  NewPasswordAccount,
  Order,
  Page,
  PersonalToken,
  Priority,
  ProviderType,
  QuietHours,
  Registration,
  ReminderLogEntry,
  ReminderPlan,
  Role,
  ScheduledReminder,
  Settings,
  Subitem,
  User,
  UserConnection,
  UserSummary,
} from "./types.js";

export interface RestOptions {
  /** The address of the hub. In a browser it may be left out and is the page's own. */
  url?: string | undefined;
  /** A personal token or a connection token. */
  token?: string | undefined;
  /** "session" uses the login cookie of the dashboard instead of a token. */
  auth?: "session";
  /** false turns off waiting out a 429. */
  retry?: boolean;
  fetch?: typeof fetch;
}

export interface CallOptions {
  signal?: AbortSignal;
}

export interface UserOptions extends CallOptions {
  /** The user to act for. Defaults to the caller; a connection must name one. */
  user?: string;
}

export interface WriteOptions extends UserOptions {
  /** Comes back as `nonce` in every gateway event this call causes. */
  nonce?: string;
}

export interface AdminWriteOptions extends CallOptions {
  nonce?: string;
}

export interface ListItemsQuery {
  listId?: string;
  status?: ItemStatus | "all";
  hasDue?: boolean;
  dueBefore?: Date;
  dueAfter?: Date;
  sort?: "created" | "due" | "priority" | "completed";
  order?: "asc" | "desc";
  limit?: number;
  cursor?: string | undefined;
}

export interface UserChanges {
  displayName?: string;
  timezone?: string;
  locale?: string | null;
  preferences?: Readonly<Record<string, unknown>>;
  quietHours?: QuietHours | null;
}

export interface ProviderInput {
  type?: ProviderType;
  name?: string;
  loginEnabled?: boolean;
  linkEnabled?: boolean;
  clientId?: string;
  clientSecret?: string;
  issuerUrl?: string;
}

interface Request {
  query?: object | undefined;
  body?: unknown;
  user?: boolean;
  options?: WriteOptions | undefined;
  /** The answer is a file, not JSON. */
  raw?: boolean;
  /** A 429 is handed to the caller at once. */
  noRetry?: boolean;
}

const maxRetries = 3;
const longestWait = 30_000;

function seg(value: string): string {
  return encodeURIComponent(value);
}

/** Every call of the REST API as a method named after its operation in the API reference. */
export class RestClient {
  readonly #base: string;
  readonly #token: string | undefined;
  readonly #session: boolean;
  readonly #retry: boolean;
  readonly #fetch: typeof fetch;

  constructor(options: RestOptions = {}) {
    if (options.token !== undefined && options.auth !== undefined) {
      throw new TypeError('Give either a token or auth: "session", not both.');
    }
    const origin = (globalThis as { location?: { origin?: string } }).location?.origin;
    const url = options.url ?? origin;
    if (!url) throw new TypeError("The address of the hub is missing: pass { url }.");
    this.#base = `${url.replace(/\/+$/, "")}/api/v1`;
    this.#token = options.token;
    this.#session = options.auth === "session";
    this.#retry = options.retry !== false;
    this.#fetch = options.fetch ?? ((input, init) => fetch(input, init));
  }

  async #request<T>(method: string, path: string, request: Request = {}): Promise<T> {
    const options = request.options;
    let target = path;
    if (request.user) {
      const named = typeof options?.user === "string" ? options.user : undefined;
      const user = named === "@me" ? undefined : named;
      if (user === undefined && this.#token?.startsWith("plsr_con_")) {
        throw new TypeError("A connection must say which user it acts for: pass { user }.");
      }
      target = `/users/${user === undefined ? "@me" : seg(user)}${path}`;
    }
    // An empty or dot segment would be folded away on the way out and reach another call.
    if (
      target
        .split("/")
        .slice(1)
        .some((part) => part === "" || part === "." || part === "..")
    ) {
      throw new TypeError("An id in the path is empty or a dot segment.");
    }
    const url = new URL(this.#base + target);
    if (request.query !== null && typeof request.query === "object") {
      for (const [key, value] of Object.entries(toWire(request.query) as object)) {
        if (value !== null) url.searchParams.set(key, String(value));
      }
    }
    const headers: Record<string, string> = { accept: request.raw ? "*/*" : "application/json" };
    if (this.#token !== undefined) headers.authorization = `Bearer ${this.#token}`;
    if (typeof options?.nonce === "string") headers["x-request-nonce"] = options.nonce;
    const init: RequestInit = { method, headers };
    if (request.body !== undefined) {
      headers["content-type"] = "application/json";
      init.body = JSON.stringify(toWire(request.body));
    }
    if (this.#session) init.credentials = "include";
    const signal = options?.signal instanceof AbortSignal ? options.signal : undefined;
    if (signal) init.signal = signal;

    for (let attempt = 0; ; attempt++) {
      let response: Response;
      try {
        response = await this.#fetch(url.toString(), init);
      } catch (cause) {
        if (signal?.aborted) throw cause;
        throw new NetworkError(`The hub did not answer ${method} ${target}.`, cause);
      }
      if (response.status === 429 && this.#retry && !request.noRetry && attempt < maxRetries) {
        const asked = response.headers.get("retry-after");
        const wait = asked === null || asked.trim() === "" ? Number.NaN : Number(asked) * 1000;
        if (wait >= 0 && wait < longestWait) {
          await response.body?.cancel().catch(() => {});
          await pause(wait, signal);
          continue;
        }
      }
      if (request.raw && response.ok) {
        try {
          return (await response.blob()) as T;
        } catch (cause) {
          if (signal?.aborted) throw cause;
          throw new NetworkError(`The answer to ${method} ${target} broke off.`, cause);
        }
      }
      return read<T>(response, method, target, signal);
    }
  }

  /** Walks a listing page by page. Pass a function that fetches the page after a cursor. */
  async *pages<T>(
    fetchPage: (cursor: string | undefined) => Promise<Page<T>>,
  ): AsyncGenerator<Page<T>, void, undefined> {
    let cursor: string | undefined;
    do {
      const page = await fetchPage(cursor);
      yield page;
      cursor = page.nextCursor ?? undefined;
    } while (cursor !== undefined);
  }

  /** The address to send the browser to for logging in with, or linking, an account elsewhere. */
  oauthStartUrl(
    provider: string,
    options: { intent?: "login" | "link"; inviteCode?: string } = {},
  ): string {
    const url = new URL(`${this.#base}/auth/oauth/${seg(provider)}/start`);
    if (options.intent !== undefined) url.searchParams.set("intent", options.intent);
    if (options.inviteCode !== undefined) url.searchParams.set("invite_code", options.inviteCode);
    return url.toString();
  }

  async getInfo(options?: CallOptions): Promise<Info> {
    const info = await this.#request<Info>("GET", "/info", { options });
    // Hubs from before login and link listed only the methods that log in.
    const loginMethods = (info.loginMethods as readonly Partial<LoginMethod>[] | undefined)?.map(
      (method) => ({ ...method, login: method.login ?? true, link: method.link ?? false }),
    );
    return loginMethods ? { ...info, loginMethods: loginMethods as LoginMethod[] } : info;
  }

  setup(account: NewPasswordAccount, options?: CallOptions): Promise<User> {
    return this.#request("POST", "/setup", { body: account, options });
  }

  login(credentials: { username: string; password: string }, options?: CallOptions): Promise<User> {
    return this.#request("POST", "/auth/login", { body: credentials, options });
  }

  logout(options?: CallOptions): Promise<void> {
    return this.#request("POST", "/auth/logout", { options });
  }

  register(
    account: NewPasswordAccount & { inviteCode?: string },
    options?: CallOptions,
  ): Promise<User> {
    return this.#request("POST", "/auth/register", { body: account, options });
  }

  getUser(options?: UserOptions): Promise<User | UserSummary> {
    return this.#request("GET", "", { user: true, options });
  }

  updateUser(changes: UserChanges, options?: WriteOptions): Promise<User> {
    return this.#request("PATCH", "", { user: true, body: changes, options });
  }

  deleteUser(options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", "", { user: true, options });
  }

  listIdentities(options?: UserOptions): Promise<Identity[]> {
    return this.#request("GET", "/identities", { user: true, options });
  }

  /** The address of a linked account's picture, for an `<img>` in a browser that is logged in. */
  identityAvatarUrl(id: string, options?: UserOptions): string {
    const named = typeof options?.user === "string" ? options.user : "@me";
    const user = named === "@me" ? named : seg(named);
    return `${this.#base}/users/${user}/identities/${seg(id)}/avatar`;
  }

  getIdentityAvatar(id: string, options?: UserOptions): Promise<Blob> {
    return this.#request("GET", `/identities/${seg(id)}/avatar`, {
      user: true,
      options,
      raw: true,
    });
  }

  unlinkIdentity(id: string, options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", `/identities/${seg(id)}`, { user: true, options });
  }

  setPassword(
    body: { newPassword: string; currentPassword?: string; username?: string },
    options?: WriteOptions,
  ): Promise<void> {
    return this.#request("PUT", "/password", { user: true, body, options });
  }

  getSettings(options?: CallOptions): Promise<Settings> {
    return this.#request("GET", "/admin/settings", { options });
  }

  updateSettings(
    changes: { name?: string; registration?: Registration },
    options?: CallOptions,
  ): Promise<Settings> {
    return this.#request("PATCH", "/admin/settings", { body: changes, options });
  }

  listInvites(options?: CallOptions): Promise<Invite[]> {
    return this.#request("GET", "/admin/invites", { options });
  }

  createInvite(body: { expiresIn?: number } = {}, options?: CallOptions): Promise<Invite> {
    return this.#request("POST", "/admin/invites", { body, options });
  }

  deleteInvite(id: string, options?: CallOptions): Promise<void> {
    return this.#request("DELETE", `/admin/invites/${seg(id)}`, { options });
  }

  listUsers(
    query: { limit?: number; cursor?: string | undefined } = {},
    options?: CallOptions,
  ): Promise<Page<AdminUser>> {
    return this.#request("GET", "/admin/users", { query, options });
  }

  updateUserAccess(
    id: string,
    changes: { role?: Role; disabled?: boolean },
    options?: AdminWriteOptions,
  ): Promise<AdminUser> {
    return this.#request("PATCH", `/admin/users/${seg(id)}`, { body: changes, options });
  }

  adminDeleteUser(id: string, options?: AdminWriteOptions): Promise<void> {
    return this.#request("DELETE", `/admin/users/${seg(id)}`, { options });
  }

  resetPassword(id: string, body: { newPassword: string }, options?: CallOptions): Promise<void> {
    return this.#request("PUT", `/admin/users/${seg(id)}/password`, { body, options });
  }

  listTokens(options?: UserOptions): Promise<PersonalToken[]> {
    return this.#request("GET", "/tokens", { user: true, options });
  }

  createToken(
    body: { name: string; remind?: boolean },
    options?: WriteOptions,
  ): Promise<PersonalToken> {
    return this.#request("POST", "/tokens", { user: true, body, options });
  }

  updateToken(
    id: string,
    changes: { name?: string; remind?: boolean },
    options?: WriteOptions,
  ): Promise<PersonalToken> {
    return this.#request("PATCH", `/tokens/${seg(id)}`, { user: true, body: changes, options });
  }

  deleteToken(id: string, options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", `/tokens/${seg(id)}`, { user: true, options });
  }

  listLists(options?: UserOptions): Promise<List[]> {
    return this.#request("GET", "/lists", { user: true, options });
  }

  createList(body: ListName, options?: WriteOptions): Promise<List> {
    return this.#request("POST", "/lists", { user: true, body, options });
  }

  reorderLists(order: Order, options?: WriteOptions): Promise<List[]> {
    return this.#request("PUT", "/lists/order", { user: true, body: order, options });
  }

  renameList(id: string, body: ListName, options?: WriteOptions): Promise<List> {
    return this.#request("PATCH", `/lists/${seg(id)}`, { user: true, body, options });
  }

  /** Items in the list move to the inbox. */
  deleteList(id: string, options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", `/lists/${seg(id)}`, { user: true, options });
  }

  listReminderPlans(options?: UserOptions): Promise<ReminderPlan[]> {
    return this.#request("GET", "/reminder-plans", { user: true, options });
  }

  setReminderPlan(
    priority: Priority,
    plan: { before: readonly number[]; followUp: FollowUp },
    options?: WriteOptions,
  ): Promise<ReminderPlan> {
    const path = `/reminder-plans/${seg(priority)}`;
    return this.#request("PUT", path, { user: true, body: plan, options });
  }

  resetReminderPlan(priority: Priority, options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", `/reminder-plans/${seg(priority)}`, { user: true, options });
  }

  listItems(query: ListItemsQuery = {}, options?: UserOptions): Promise<Page<Item>> {
    return this.#request("GET", "/items", { user: true, query, options });
  }

  createItem(input: ItemInput & { title: string }, options?: WriteOptions): Promise<Item> {
    return this.#request("POST", "/items", { user: true, body: input, options });
  }

  getItem(id: string, options?: UserOptions): Promise<Item> {
    return this.#request("GET", `/items/${seg(id)}`, { user: true, options });
  }

  updateItem(
    id: string,
    changes: ItemInput & { status?: ItemStatus },
    options?: WriteOptions,
  ): Promise<Item> {
    return this.#request("PATCH", `/items/${seg(id)}`, { user: true, body: changes, options });
  }

  deleteItem(id: string, options?: WriteOptions): Promise<void> {
    return this.#request("DELETE", `/items/${seg(id)}`, { user: true, options });
  }

  createSubitem(itemId: string, body: { title: string }, options?: WriteOptions): Promise<Subitem> {
    return this.#request("POST", `/items/${seg(itemId)}/subitems`, { user: true, body, options });
  }

  reorderSubitems(itemId: string, order: Order, options?: WriteOptions): Promise<Subitem[]> {
    const path = `/items/${seg(itemId)}/subitems/order`;
    return this.#request("PUT", path, { user: true, body: order, options });
  }

  updateSubitem(
    itemId: string,
    subitemId: string,
    changes: { title?: string; done?: boolean },
    options?: WriteOptions,
  ): Promise<Subitem> {
    const path = `/items/${seg(itemId)}/subitems/${seg(subitemId)}`;
    return this.#request("PATCH", path, { user: true, body: changes, options });
  }

  deleteSubitem(itemId: string, subitemId: string, options?: WriteOptions): Promise<void> {
    const path = `/items/${seg(itemId)}/subitems/${seg(subitemId)}`;
    return this.#request("DELETE", path, { user: true, options });
  }

  /** The reminders an open item still has ahead, earliest first. */
  getItemSchedule(itemId: string, options?: UserOptions): Promise<ScheduledReminder[]> {
    return this.#request("GET", `/items/${seg(itemId)}/schedule`, { user: true, options });
  }

  listReminders(itemId: string, options?: UserOptions): Promise<ManualReminder[]> {
    return this.#request("GET", `/items/${seg(itemId)}/reminders`, { user: true, options });
  }

  /** `at` is a moment, `in` a span from now in milliseconds. */
  createReminder(
    itemId: string,
    when: { at: Date } | { in: number },
    options?: WriteOptions,
  ): Promise<ManualReminder> {
    const path = `/items/${seg(itemId)}/reminders`;
    return this.#request("POST", path, { user: true, body: when, options });
  }

  deleteReminder(itemId: string, reminderId: string, options?: WriteOptions): Promise<void> {
    const path = `/items/${seg(itemId)}/reminders/${seg(reminderId)}`;
    return this.#request("DELETE", path, { user: true, options });
  }

  /** For connections: the hub user behind an account at the connection's provider. */
  lookupUser(externalId: string, options?: CallOptions): Promise<LookupResult> {
    const query = { externalId };
    return this.#request("GET", "/connection/users/lookup", { query, options });
  }

  listUserConnections(options?: UserOptions): Promise<UserConnection[]> {
    return this.#request("GET", "/connections", { user: true, options });
  }

  setConnectionGrant(
    id: string,
    grant: { enabled?: boolean; remind?: boolean },
    options?: WriteOptions,
  ): Promise<UserConnection> {
    return this.#request("PUT", `/connections/${seg(id)}`, { user: true, body: grant, options });
  }

  /** Asks the app to show that it reaches the user. Too soon after the last one is a 429 at once. */
  startConnectionTest(id: string, options?: WriteOptions): Promise<ConnectionTest> {
    // Waiting out the limit would look like a button that hangs.
    return this.#request("POST", `/connections/${seg(id)}/tests`, {
      user: true,
      options,
      noRetry: true,
    });
  }

  getConnectionTest(id: string, testId: string, options?: UserOptions): Promise<ConnectionTest> {
    return this.#request("GET", `/connections/${seg(id)}/tests/${seg(testId)}`, {
      user: true,
      options,
    });
  }

  listConnections(options?: CallOptions): Promise<Connection[]> {
    return this.#request("GET", "/admin/connections", { options });
  }

  createConnection(
    body: {
      name: string;
      provider: string;
      description?: string;
      allowedIntents?: readonly Intent[];
      installUrl?: string;
    },
    options?: CallOptions,
  ): Promise<Connection> {
    return this.#request("POST", "/admin/connections", { body, options });
  }

  updateConnection(
    id: string,
    changes: {
      name?: string;
      description?: string;
      enabled?: boolean;
      allowedIntents?: readonly Intent[];
      installUrl?: string;
    },
    options?: CallOptions,
  ): Promise<Connection> {
    return this.#request("PATCH", `/admin/connections/${seg(id)}`, { body: changes, options });
  }

  deleteConnection(id: string, options?: CallOptions): Promise<void> {
    return this.#request("DELETE", `/admin/connections/${seg(id)}`, { options });
  }

  /** The answer carries the new token. The old one stops working. */
  rotateConnectionToken(id: string, options?: CallOptions): Promise<Connection> {
    return this.#request("POST", `/admin/connections/${seg(id)}/token`, { options });
  }

  listProviders(options?: CallOptions): Promise<LoginProvider[]> {
    return this.#request("GET", "/admin/providers", { options });
  }

  setProvider(key: string, body: ProviderInput, options?: CallOptions): Promise<LoginProvider> {
    return this.#request("PUT", `/admin/providers/${seg(key)}`, { body, options });
  }

  deleteProvider(key: string, options?: CallOptions): Promise<void> {
    return this.#request("DELETE", `/admin/providers/${seg(key)}`, { options });
  }

  /** The open deliveries of the calling token, for adapters that do not use the gateway. */
  listDeliveries(options?: CallOptions): Promise<Delivery[]> {
    return this.#request("GET", "/deliveries", { options });
  }

  /** Confirms that a reminder reached the person, or says why not, which the user gets to see. */
  acknowledgeDelivery(
    id: string,
    outcome: { status: "delivered" } | { status: "failed"; reason: string },
    options?: CallOptions,
  ): Promise<DeliveryResult> {
    return this.#request("PUT", `/deliveries/${seg(id)}`, { body: outcome, options });
  }

  /** Says how a test the user asked for went, which the user gets to see. */
  reportTest(
    id: string,
    outcome: { status: "delivered" } | { status: "failed"; reason: string },
    options?: CallOptions,
  ): Promise<ConnectionTest> {
    return this.#request("PUT", `/deliveries/tests/${seg(id)}`, { body: outcome, options });
  }

  listReminderLog(
    query: { itemId?: string; limit?: number; cursor?: string | undefined } = {},
    options?: UserOptions,
  ): Promise<Page<ReminderLogEntry>> {
    return this.#request("GET", "/reminder-log", { user: true, query, options });
  }
}

async function read<T>(
  response: Response,
  method: string,
  target: string,
  signal: AbortSignal | undefined,
): Promise<T> {
  let text: string;
  try {
    text = await response.text();
  } catch (cause) {
    if (signal?.aborted) throw cause;
    throw new NetworkError(`The answer to ${method} ${target} broke off.`, cause);
  }
  let parsed: unknown;
  let readable = text !== "" || !response.ok || response.status === 204;
  if (text !== "") {
    try {
      parsed = JSON.parse(text);
    } catch {
      readable = false;
    }
  }
  if (!response.ok) {
    const error = (parsed as { error?: Record<string, unknown> } | undefined)?.error;
    if (error !== null && typeof error === "object" && typeof error.code === "string") {
      throw new ApiError(
        response.status,
        error.code,
        typeof error.message === "string" ? error.message : error.code,
        error.fields !== null && typeof error.fields === "object"
          ? camelKeys(error.fields as Record<string, string>)
          : undefined,
      );
    }
    throw new ApiError(response.status, "unknown", `The hub answered ${response.status}.`);
  }
  if (!readable) {
    throw new NetworkError(`The answer to ${method} ${target} was empty or not JSON.`);
  }
  return fromWire<T>(parsed);
}

function pause(ms: number, signal: AbortSignal | undefined): Promise<void> {
  return new Promise((resolve, reject) => {
    signal?.throwIfAborted();
    const timer = setTimeout(done, ms);
    function done() {
      signal?.removeEventListener("abort", aborted);
      resolve();
    }
    function aborted() {
      clearTimeout(timer);
      reject(signal!.reason);
    }
    signal?.addEventListener("abort", aborted, { once: true });
  });
}
