import { useCallback, useEffect, useRef, useState, type SetStateAction } from "react";
import { useNotices } from "../shell/notices.js";

/** Data that is not in the cache: fetched when shown, and again on request. */
export function useLoaded<T>(load: (signal: AbortSignal) => Promise<T>, key: unknown) {
  const { guard } = useNotices();
  const [data, setState] = useState<T>();
  const [failed, setFailed] = useState(false);
  const [round, setRound] = useState(0);
  const changes = useRef(0);
  const loadedFor = useRef(key);

  useEffect(() => {
    const abort = new AbortController();
    const before = changes.current;
    // What was loaded for another account must not stay on the page.
    if (loadedFor.current !== key) setState(undefined);
    loadedFor.current = key;
    setFailed(false);
    const loading = load(abort.signal).catch((error: unknown) => {
      if (abort.signal.aborted) return undefined;
      setFailed(true);
      return Promise.reject(error);
    });
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
  return { data, setData, reload, failed: failed && data === undefined };
}

/** What useLoaded gives: the data once it is there, and ways to change or reload it. */
export type Loaded<T> = ReturnType<typeof useLoaded<T>>;
