import { useCallback, useEffect, useState } from "react";
import { useNotices } from "../shell/notices.js";

/** Data that is not in the cache: fetched when shown, and again on request. */
export function useLoaded<T>(load: (signal: AbortSignal) => Promise<T>, key: unknown) {
  const { guard } = useNotices();
  const [data, setData] = useState<T>();
  const [round, setRound] = useState(0);

  useEffect(() => {
    const abort = new AbortController();
    void guard(
      load(abort.signal).catch((error) =>
        abort.signal.aborted ? undefined : Promise.reject(error),
      ),
    ).then((loaded) => {
      if (loaded !== undefined && !abort.signal.aborted) setData(loaded);
    });
    return () => abort.abort();
    // The loader is rebuilt on every render; the key says when its answer would differ.
  }, [key, round, guard]);

  const reload = useCallback(() => setRound((before) => before + 1), []);
  return { data, setData, reload };
}
