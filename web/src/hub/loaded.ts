import { useCallback, useEffect, useRef, useState, type SetStateAction } from "react";
import { useNotices } from "../shell/notices.js";

/** Data that is not in the cache: fetched when shown, and again on request. */
export function useLoaded<T>(load: (signal: AbortSignal) => Promise<T>, key: unknown) {
  const { guard } = useNotices();
  const [data, setState] = useState<T>();
  const [round, setRound] = useState(0);
  const changes = useRef(0);

  useEffect(() => {
    const abort = new AbortController();
    const before = changes.current;
    const loading = load(abort.signal).catch((error: unknown) =>
      abort.signal.aborted ? undefined : Promise.reject(error),
    );
    void guard(loading).then((loaded) => {
      if (loaded === undefined || abort.signal.aborted) return;
      // An answer from before a change made here would undo it on the page.
      if (changes.current !== before) setRound((now) => now + 1);
      else setState(loaded);
    });
    return () => abort.abort();
    // The loader is rebuilt on every render; the key says when its answer would differ.
  }, [key, round, guard]);

  const setData = useCallback((next: SetStateAction<T | undefined>) => {
    changes.current++;
    setState(next);
  }, []);
  const reload = useCallback(() => setRound((before) => before + 1), []);
  return { data, setData, reload };
}
