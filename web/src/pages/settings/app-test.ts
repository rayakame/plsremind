import { ApiError, type ConnectionTest } from "plsremind";
import { useCallback, useEffect, useRef, useState } from "react";
import { describeError } from "../../hub/errors.js";
import { useHub } from "../../hub/session.js";
import { t } from "../../i18n/index.js";

/** How long to wait before looking at a test again: often at first, then less, to spare the rate limit. */
export function pollDelay(round: number): number {
  return round < 5 ? 1000 : 3000;
}

// A few failed looks in a row mean the answer is out of reach for now.
const mostMissed = 3;
// The hub gives an app a minute to answer; a little more covers the round trip.
const pollFor = 70_000;

export type TestState =
  | { readonly status: "starting" }
  | { readonly status: "refused"; readonly message: string }
  | { readonly status: "lost" }
  | ConnectionTest;

/** Whether a test is still under way, so that another must wait. */
export function isRunning(state: TestState | undefined): boolean {
  return state?.status === "starting" || state?.status === "pending";
}

/** Starts tests of apps and follows each until the app answers, then calls done. */
export function useAppTest(done: () => void) {
  const { client } = useHub();
  const [states, setStates] = useState<Readonly<Record<string, TestState>>>({});
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const start = useCallback(
    async (id: string) => {
      const show = (state: TestState) => {
        if (alive.current) setStates((before) => ({ ...before, [id]: state }));
      };
      show({ status: "starting" });
      let test: ConnectionTest;
      try {
        test = await client.rest.startConnectionTest(id);
      } catch (error) {
        const waiting = error instanceof ApiError && error.code === "rate_limited";
        show({ status: "refused", message: waiting ? t("apps.testWait") : describeError(error) });
        return;
      }
      show(test);
      const until = Date.now() + pollFor;
      let missed = 0;
      for (
        let round = 0;
        test.status === "pending" && Date.now() < until && missed < mostMissed;
        round++
      ) {
        await new Promise((resolve) => setTimeout(resolve, pollDelay(round)));
        if (!alive.current) return;
        try {
          test = await client.rest.getConnectionTest(id, test.id);
          missed = 0;
        } catch {
          missed += 1;
          continue;
        }
        show(test);
      }
      if (test.status === "pending") show({ status: "lost" });
      if (alive.current) done();
    },
    [client, done],
  );

  return { state: (id: string): TestState | undefined => states[id], start };
}
