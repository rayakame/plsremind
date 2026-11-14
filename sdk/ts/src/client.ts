import { CacheStore, type Cache } from "./cache.js";
import { fromWire } from "./convert.js";
import { Emitter } from "./emitter.js";
import { GatewayError } from "./errors.js";
import { hubEventNames, type EventMap, type Principal, type ReadyEvent } from "./events.js";
import { Gateway, type SocketLike } from "./gateway.js";
import { RestClient, type RestOptions } from "./rest.js";
import type { Intent } from "./types.js";

export interface ClientOptions extends RestOptions {
  /** What the gateway session should receive. Without it the client can only use REST. */
  intents?: readonly Intent[];
}

export type ClientStatus = "idle" | "connecting" | "connected" | "reconnecting" | "closed";

export interface ClientInternals {
  createSocket?: (url: string) => SocketLike;
  random?: () => number;
}

const hubEvents: ReadonlySet<string> = new Set(hubEventNames);
const withoutOld: ReadonlySet<string> = new Set([
  "user.sync",
  "user.gone",
  "delivery.created",
  "delivery.closed",
]);

type Listen = (name: string, listener: () => void) => void;

/** A gateway session with a cache of what it covers, typed events and the REST API. */
export class Client {
  /** Every call of the REST API. Answers do not change the cache; the events that follow do. */
  readonly rest: RestClient;
  /** The state the session covers, kept current by the gateway. */
  readonly cache: Cache;

  readonly #store = new CacheStore();
  readonly #events = new Emitter<EventMap>();
  readonly #options: ClientOptions;
  readonly #internals: ClientInternals;
  #status: ClientStatus = "idle";
  #principal: Principal | undefined;
  #intents: readonly Intent[] | undefined;
  #gateway: Gateway | undefined;
  #connecting: Promise<void> | undefined;
  #settle: { resolve: () => void; reject: (error: unknown) => void } | undefined;
  #ready: ReadyEvent | undefined;
  #waiting = new Set<string>();
  #unhook: (() => void) | undefined;

  constructor(options: ClientOptions = {}, internals: ClientInternals = {}) {
    this.#options = options;
    this.#internals = internals;
    this.rest = new RestClient(options);
    this.cache = this.#store.cache;
  }

  /** Who the client is logged in as. Set once the gateway said READY. */
  get principal(): Principal | undefined {
    return this.#principal;
  }

  /** The intents of the session, or the ones asked for while there is none yet. */
  get intents(): readonly Intent[] {
    return this.#intents ?? this.#options.intents ?? [];
  }

  get status(): ClientStatus {
    return this.#status;
  }

  /**
   * Opens the gateway session. Resolves once the state of every user it covers is in the
   * cache, and rejects with a GatewayError if the hub cannot be reached or refuses the login.
   * Later drops are handled without the caller doing anything.
   */
  connect(): Promise<void> {
    if (this.#connecting) return this.#connecting;
    const intents = this.#options.intents;
    if (!intents || intents.length === 0) {
      return Promise.reject(new TypeError("connect() needs intents: pass { intents: [...] }."));
    }
    const createSocket =
      this.#internals.createSocket ?? ((url) => new WebSocket(url) as unknown as SocketLike);
    const gateway = new Gateway({
      url: async () => (await this.rest.getInfo()).gatewayUrl,
      token: this.#options.token,
      intents,
      createSocket,
      random: this.#internals.random ?? Math.random,
      onDispatch: (name, data) => this.#dispatch(name, data),
      onInvalidated: () => {},
      onDisconnected: (code, reason) => {
        this.#emit("disconnected", { code, reason });
      },
      onClosed: (error) => this.#closed(error),
      onStatus: (status) => {
        this.#status = status;
      },
    });
    this.#gateway = gateway;
    this.#connecting = new Promise((resolve, reject) => {
      this.#settle = { resolve, reject };
    });
    this.#hook(gateway);
    gateway.start();
    return this.#connecting;
  }

  /** Ends the session and every retry. The cache stays readable; connect() starts over. */
  close(): void {
    if (!this.#gateway) return;
    this.#teardown();
    this.#status = "idle";
    this.#settle?.reject(new GatewayError("The client was closed."));
    this.#settle = undefined;
  }

  /** Calls the handler for every event of that name. Returns a function that unsubscribes. */
  on<K extends keyof EventMap>(name: K, handler: (event: EventMap[K]) => unknown): () => void {
    return this.#events.on(name, handler);
  }

  /** Like on(), for the next event of that name only. */
  once<K extends keyof EventMap>(name: K, handler: (event: EventMap[K]) => unknown): () => void {
    return this.#events.once(name, handler);
  }

  #teardown(): void {
    this.#gateway?.stop();
    this.#gateway = undefined;
    this.#connecting = undefined;
    this.#ready = undefined;
    this.#waiting.clear();
    this.#unhook?.();
    this.#unhook = undefined;
  }

  #closed(error: GatewayError): void {
    this.#teardown();
    this.#status = "closed";
    const settle = this.#settle;
    this.#settle = undefined;
    this.#emit("closed", { code: error.code, reason: error.message });
    settle?.reject(error);
  }

  #dispatch(name: string, data: unknown): void {
    if (name === "READY") {
      const ready = fromWire<ReadyEvent>(data);
      this.#principal = ready.principal;
      this.#intents = ready.intents;
      this.#ready = { principal: ready.principal, users: ready.users, intents: ready.intents };
      this.#waiting = new Set(ready.users);
      const self = "userId" in ready.principal ? ready.principal.userId : undefined;
      this.#store.begin(ready.users, ready.intents, self);
      this.#arrived();
      return;
    }
    if (name === "RESUMED") {
      this.#emit("resumed", {});
      return;
    }
    if (!hubEvents.has(name)) return;
    const applied = this.#store.apply(name, data);
    const event = withoutOld.has(name)
      ? applied.event
      : { ...(applied.event as object), old: applied.old };
    this.#emit(name as keyof EventMap, event as never);
    const userId = (event as { userId?: string }).userId;
    if (applied.synced !== undefined) {
      this.#emit("user.ready", { userId: applied.synced });
      this.#arrived(applied.synced);
    } else if (name === "user.gone" && userId !== undefined) {
      this.#arrived(userId);
    }
  }

  #arrived(userId?: string): void {
    if (userId !== undefined && !this.#waiting.delete(userId)) return;
    if (this.#waiting.size > 0 || !this.#ready) return;
    const ready = this.#ready;
    this.#ready = undefined;
    this.#emit("ready", ready);
    this.#settle?.resolve();
    this.#settle = undefined;
  }

  #emit<K extends keyof EventMap>(name: K, event: EventMap[K]): void {
    this.#events.emit(name, event, (error) => {
      if (name === "error" || !this.#events.has("error")) {
        console.error(`plsremind: a handler for "${name}" failed`, error);
        return;
      }
      this.#emit("error", { error, event: name });
    });
  }

  #hook(gateway: Gateway): void {
    const page = globalThis as {
      addEventListener?: Listen;
      removeEventListener?: Listen;
      document?: {
        visibilityState?: string;
        addEventListener?: Listen;
        removeEventListener?: Listen;
      };
    };
    if (typeof page.addEventListener !== "function") return;
    const online = () => gateway.wake();
    const visible = () => {
      if (page.document?.visibilityState === "visible") gateway.wake();
    };
    page.addEventListener("online", online);
    page.document?.addEventListener?.("visibilitychange", visible);
    this.#unhook = () => {
      page.removeEventListener?.("online", online);
      page.document?.removeEventListener?.("visibilitychange", visible);
    };
  }
}
