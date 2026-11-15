import { GatewayError } from "./errors.js";

export interface SocketLike {
  send(text: string): void;
  close(code?: number): void;
  onopen: (() => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code: number; reason: string }) => void) | null;
  onerror: (() => void) | null;
}

export interface GatewayOptions {
  url: () => Promise<string>;
  token: string | undefined;
  intents: readonly string[];
  createSocket: (url: string) => SocketLike;
  random: () => number;
  onDispatch: (name: string, data: unknown) => void;
  onDisconnected: (code: number | undefined, reason: string) => void;
  onClosed: (error: GatewayError) => void;
  onStatus: (status: "connecting" | "connected" | "reconnecting") => void;
}

const opDispatch = 0;
const opHeartbeat = 1;
const opIdentify = 2;
const opResume = 6;
const opReconnect = 7;
const opInvalidSession = 9;
const opHello = 10;
const opHeartbeatAck = 11;

const closeNormal = 1000;
const closeResuming = 4000;
const closeBadSequence = 4007;
const closeTooMany = 4008;
const fatal = new Set([4004, 4013, 4014]);

const firstWait = 1000;
const longestWait = 60_000;
const waitAfterTooMany = 5000;
const silenceWhileLoggingIn = 15_000;
const answerProbeWithin = 5000;

/** One gateway session kept alive across drops. It knows nothing about what the events mean. */
export class Gateway {
  readonly #options: GatewayOptions;
  #running = false;
  #generation = 0;
  #address: string | undefined;
  #socket: SocketLike | undefined;
  #session: string | undefined;
  #seq: number | null = null;
  #established = false;
  #attempts = 0;
  #acked = true;
  #heartbeat: ReturnType<typeof setTimeout> | undefined;
  #wait: ReturnType<typeof setTimeout> | undefined;
  #login: ReturnType<typeof setTimeout> | undefined;
  #probe: ReturnType<typeof setTimeout> | undefined;
  #online = false;
  #greeted = false;

  constructor(options: GatewayOptions) {
    this.#options = options;
  }

  start(): void {
    if (this.#running) return;
    this.#running = true;
    this.#options.onStatus("connecting");
    void this.#open();
  }

  stop(): void {
    this.#running = false;
    this.#generation++;
    this.#release(closeNormal);
    clearTimeout(this.#wait);
    this.#wait = undefined;
    this.#session = undefined;
    this.#seq = null;
    this.#established = false;
    this.#online = false;
    this.#attempts = 0;
  }

  /** Connects now instead of sitting out the rest of a wait. */
  wake(): void {
    if (!this.#running) return;
    if (this.#wait !== undefined) {
      clearTimeout(this.#wait);
      this.#wait = undefined;
      void this.#open();
    } else if (this.#login !== undefined && !this.#greeted && this.#established) {
      this.#release(closeResuming);
      void this.#open();
    } else if (this.#socket && this.#login === undefined && this.#probe === undefined) {
      // After sleep or a network change the socket may be dead without a close event.
      const socket = this.#socket;
      this.#acked = false;
      this.#send(socket, opHeartbeat, this.#seq);
      this.#probe = setTimeout(() => {
        this.#probe = undefined;
        if (this.#socket !== socket || this.#acked) return;
        this.#release(closeResuming);
        this.#lost(undefined, "The hub stopped answering heartbeats.");
      }, answerProbeWithin);
    }
  }

  async #open(): Promise<void> {
    if (!this.#running) return;
    const generation = ++this.#generation;
    let socket: SocketLike;
    try {
      this.#address ??= await this.#options.url();
      if (generation !== this.#generation) return;
      socket = this.#options.createSocket(this.#address);
    } catch (cause) {
      if (generation !== this.#generation) return;
      const why = cause instanceof Error ? cause.message : String(cause);
      this.#lost(undefined, `The gateway could not be reached: ${why}`, cause);
      return;
    }
    this.#socket = socket;
    this.#greeted = false;
    this.#expectLogin(socket);
    socket.onmessage = (event) => {
      if (this.#socket === socket) this.#receive(socket, event.data);
    };
    socket.onclose = (event) => {
      if (this.#socket !== socket) return;
      this.#release();
      this.#lost(event.code, event.reason);
    };
  }

  #release(code?: number): void {
    clearTimeout(this.#heartbeat);
    clearTimeout(this.#login);
    clearTimeout(this.#probe);
    this.#heartbeat = this.#login = this.#probe = undefined;
    const socket = this.#socket;
    this.#socket = undefined;
    if (!socket) return;
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null;
    if (code !== undefined) {
      try {
        socket.close(code);
      } catch {
        // already closed
      }
    }
  }

  #lost(code: number | undefined, reason: string, cause?: unknown): void {
    if (code !== undefined && fatal.has(code)) {
      this.#end(new GatewayError(reason || `The gateway closed with ${code}.`, code));
      return;
    }
    if (!this.#established) {
      const message = reason || "The gateway connection could not be established.";
      this.#end(new GatewayError(message, code, cause));
      return;
    }
    if (code === closeBadSequence) this.#forget();
    if (!this.#disconnected(code, reason)) return;
    const step = Math.min(longestWait, firstWait * 2 ** this.#attempts);
    this.#attempts++;
    let wait = step * (0.5 + 0.5 * this.#options.random());
    if (code === closeTooMany) wait = Math.max(wait, waitAfterTooMany);
    this.#wait = setTimeout(() => {
      this.#wait = undefined;
      void this.#open();
    }, wait);
  }

  /** Reports the start of an outage. False if the callback stopped the gateway. */
  #disconnected(code: number | undefined, reason: string): boolean {
    if (!this.#online) return true;
    this.#online = false;
    const generation = this.#generation;
    this.#options.onStatus("reconnecting");
    this.#options.onDisconnected(code, reason);
    return this.#running && generation === this.#generation;
  }

  // A socket that goes quiet before the session stands would otherwise be waited on forever.
  #expectLogin(socket: SocketLike): void {
    clearTimeout(this.#login);
    this.#login = setTimeout(() => {
      this.#login = undefined;
      if (this.#socket !== socket) return;
      this.#release(closeResuming);
      this.#lost(undefined, "The hub did not finish the handshake.");
    }, silenceWhileLoggingIn);
  }

  #end(error: GatewayError): void {
    this.stop();
    this.#options.onClosed(error);
  }

  #forget(): void {
    this.#session = undefined;
    this.#seq = null;
  }

  #receive(socket: SocketLike, raw: unknown): void {
    let frame: { op?: unknown; d?: unknown; s?: unknown; t?: unknown } | null;
    try {
      frame = JSON.parse(String(raw));
    } catch {
      return;
    }
    if (frame === null || typeof frame !== "object") return;
    if (this.#login !== undefined) this.#expectLogin(socket);
    switch (frame.op) {
      case opHello:
        this.#greeted = true;
        this.#beat(socket, (frame.d as { heartbeat_interval: number }).heartbeat_interval);
        this.#identify(socket);
        break;
      case opHeartbeatAck:
        this.#acked = true;
        break;
      case opReconnect:
        this.#release(closeResuming);
        if (this.#disconnected(undefined, "The hub asked to reconnect.")) void this.#open();
        break;
      case opInvalidSession:
        this.#forget();
        this.#expectLogin(socket);
        this.#identify(socket);
        break;
      case opDispatch:
        if (typeof frame.t === "string") this.#dispatch(frame.t, frame.d, frame.s);
        break;
    }
  }

  #dispatch(name: string, data: unknown, seq: unknown): void {
    if (typeof seq === "number") this.#seq = seq;
    if (name === "READY") {
      this.#session = (data as { session_id: string }).session_id;
    }
    if (name === "READY" || name === "RESUMED") {
      clearTimeout(this.#login);
      this.#login = undefined;
      this.#established = true;
      this.#online = true;
      this.#attempts = 0;
      this.#options.onStatus("connected");
    }
    this.#options.onDispatch(name, data);
  }

  #identify(socket: SocketLike): void {
    const token = this.#options.token === undefined ? {} : { token: this.#options.token };
    if (this.#session === undefined) {
      this.#send(socket, opIdentify, { ...token, intents: this.#options.intents });
    } else {
      this.#send(socket, opResume, { ...token, session_id: this.#session, seq: this.#seq });
    }
  }

  #beat(socket: SocketLike, interval: number): void {
    clearTimeout(this.#heartbeat);
    this.#acked = true;
    const beat = () => {
      if (this.#socket !== socket) return;
      if (this.#probe !== undefined) {
        this.#heartbeat = setTimeout(beat, interval);
        return;
      }
      if (!this.#acked) {
        this.#release(closeResuming);
        this.#lost(undefined, "The hub stopped answering heartbeats.");
        return;
      }
      this.#acked = false;
      this.#send(socket, opHeartbeat, this.#seq);
      this.#heartbeat = setTimeout(beat, interval);
    };
    this.#heartbeat = setTimeout(beat, interval * this.#options.random());
  }

  #send(socket: SocketLike, op: number, d: unknown): void {
    try {
      socket.send(JSON.stringify({ op, d }));
    } catch {
      // The close that follows a dead socket starts the reconnect.
    }
  }
}
