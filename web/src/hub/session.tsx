import { ApiError, Client, type Info, type RestClient } from "plsremind";
import { optimistic, type OptimisticView } from "plsremind/optimistic";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export type SessionState =
  | { phase: "loading" }
  | { phase: "unreachable" }
  | { phase: "setup"; info: Info; rest: RestClient }
  | { phase: "signedOut"; info: Info; rest: RestClient; asked: boolean }
  | { phase: "ready"; info: Info; client: Client; view: OptimisticView; userId: string };

const longestWait = 30_000;

/** Owns the client: finds out who is signed in, connects, and starts over when the session ends. */
export class Session {
  state: SessionState = { phase: "loading" };
  readonly #make: () => Client;
  readonly #listeners = new Set<() => void>();
  #client: Client | undefined;
  #generation = 0;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #asked = false;

  constructor(make: () => Client) {
    this.#make = make;
  }

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  /** Finds out again who is signed in, for example after a login or a refused call. */
  reload = (): void => {
    void this.#boot(0);
  };

  stop(): void {
    this.#generation++;
    clearTimeout(this.#timer);
    this.#client?.close();
    this.#client = undefined;
  }

  async signOut(): Promise<void> {
    await this.#client?.rest.logout();
    this.#asked = true;
    this.reload();
  }

  /** Takes over a new name of the instance without starting over. */
  rename(name: string): void {
    if (this.state.phase === "loading" || this.state.phase === "unreachable") return;
    this.#set({ ...this.state, info: { ...this.state.info, name } });
  }

  #set(state: SessionState): void {
    this.state = state;
    for (const listener of [...this.#listeners]) listener();
  }

  async #boot(attempt: number): Promise<void> {
    const generation = ++this.#generation;
    const stale = () => generation !== this.#generation;
    clearTimeout(this.#timer);
    const shown = this.state.phase === "ready" ? this.state.client : undefined;
    if (this.#client !== shown) this.#client?.close();
    const client = (this.#client = this.#make());
    let connected = false;
    try {
      const info = await client.rest.getInfo();
      if (stale()) return;
      if (info.setupRequired) {
        shown?.close();
        return this.#set({ phase: "setup", info, rest: client.rest });
      }
      try {
        await client.rest.getUser();
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 401) throw error;
        if (stale()) return;
        shown?.close();
        const asked = this.#asked;
        this.#asked = false;
        return this.#set({ phase: "signedOut", info, rest: client.rest, asked });
      }
      if (stale()) return;
      const view = optimistic(client);
      const show = () => {
        if (stale()) return;
        const user = view.user();
        if (user) {
          if (this.state.phase === "ready" && this.state.client === client) return;
          shown?.close();
          this.#set({ phase: "ready", info, client, view, userId: user.userId });
        } else if (this.state.phase === "ready" && this.state.client === client) {
          void this.#boot(0);
        }
      };
      view.subscribe(show);
      client.on("closed", () => {
        if (!stale() && connected) void this.#boot(0);
      });
      await client.connect();
      connected = true;
      show();
    } catch {
      if (stale()) return;
      client.close();
      if (this.state.phase !== "ready") this.#set({ phase: "unreachable" });
      const wait = Math.min(longestWait, 1000 * 2 ** attempt);
      this.#timer = setTimeout(() => void this.#boot(attempt + 1), wait);
    }
  }
}

const SessionContext = createContext<Session | undefined>(undefined);

function defaultClient(): Client {
  return new Client({ auth: "session", intents: ["users", "lists", "items", "plans", "history"] });
}

export function SessionProvider(props: { make?: (() => Client) | undefined; children: ReactNode }) {
  const [session] = useState(() => new Session(props.make ?? defaultClient));
  useEffect(() => {
    session.reload();
    return () => session.stop();
  }, [session]);
  return <SessionContext.Provider value={session}>{props.children}</SessionContext.Provider>;
}

export function useSessionStore(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error("no session");
  return session;
}

export function useSession(): SessionState {
  const session = useSessionStore();
  return useSyncExternalStore(session.subscribe, () => session.state);
}

export interface Hub {
  client: Client;
  view: OptimisticView;
  userId: string;
  info: Info;
}

/** The client and its view. Only for components below the signed-in shell. */
export function useHub(): Hub {
  const state = useSession();
  if (state.phase !== "ready") throw new Error("not signed in");
  return state;
}
