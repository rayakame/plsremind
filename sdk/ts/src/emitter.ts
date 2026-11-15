type Handler<E> = (event: E) => unknown;

export class Emitter<M> {
  readonly #handlers = new Map<keyof M, Set<Handler<never>>>();

  on<K extends keyof M>(name: K, handler: Handler<M[K]>): () => void {
    const own: Handler<M[K]> = (event) => handler(event);
    let handlers = this.#handlers.get(name);
    if (!handlers) this.#handlers.set(name, (handlers = new Set()));
    handlers.add(own as Handler<never>);
    return () => {
      handlers.delete(own as Handler<never>);
    };
  }

  once<K extends keyof M>(name: K, handler: Handler<M[K]>): () => void {
    const off = this.on(name, (event) => {
      off();
      return handler(event);
    });
    return off;
  }

  has(name: keyof M): boolean {
    return (this.#handlers.get(name)?.size ?? 0) > 0;
  }

  /** Runs the handlers in the order they were added. Promises are not waited for. */
  emit<K extends keyof M>(name: K, event: M[K], onError: (error: unknown) => void): void {
    const handlers = this.#handlers.get(name);
    if (!handlers) return;
    for (const handler of [...handlers] as Handler<M[K]>[]) {
      try {
        const result = handler(event);
        if (result instanceof Promise) result.catch(onError);
      } catch (error) {
        onError(error);
      }
    }
  }
}
