import type { SocketLike } from "../src/gateway.js";

type Wire = Record<string, unknown>;

export class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  readonly sent: Wire[] = [];
  closedWith: number | undefined;
  isOpen = false;

  constructor(readonly url: string) {}

  send(text: string): void {
    this.sent.push(JSON.parse(text));
  }

  close(code?: number): void {
    this.closedWith = code ?? 1005;
  }

  open(): void {
    this.isOpen = true;
    this.onopen?.();
  }

  raw(text: string): void {
    this.onmessage?.({ data: text });
  }

  op(op: number, d: unknown = null): void {
    this.raw(JSON.stringify({ op, d, s: null, t: null }));
  }

  hello(interval = 45_000): void {
    this.op(10, { heartbeat_interval: interval });
  }

  dispatch(name: string, data: unknown, seq: number | null = null): void {
    this.raw(JSON.stringify({ op: 0, t: name, s: seq, d: data }));
  }

  drop(code = 1006, reason = ""): void {
    this.onclose?.({ code, reason });
  }

  beats(): Wire[] {
    return this.sent.filter((message) => message.op === 1);
  }
}

export class FakeGateway {
  readonly sockets: FakeSocket[] = [];

  readonly createSocket = (url: string): FakeSocket => {
    const socket = new FakeSocket(url);
    this.sockets.push(socket);
    return socket;
  };

  get current(): FakeSocket {
    const socket = this.sockets.at(-1);
    if (!socket) throw new Error("no socket was opened");
    return socket;
  }

  /** Lets pending promises settle, then opens the newest socket if it is not open yet. */
  async opened(): Promise<FakeSocket> {
    await settle();
    const socket = this.current;
    if (!socket.isOpen) socket.open();
    return socket;
  }
}

export async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

export function ready(
  socket: FakeSocket,
  options: { users?: string[]; intents?: string[]; principal?: Wire; session?: string } = {},
  seq = 1,
): void {
  socket.dispatch(
    "READY",
    {
      session_id: options.session ?? "s1",
      intents: options.intents ?? ["users", "lists", "items", "plans", "history", "deliveries"],
      principal: options.principal ?? { kind: "personal_token", id: "p1", user_id: "u1" },
      users: options.users ?? ["u1"],
    },
    seq,
  );
}
