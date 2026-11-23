import { ApiError, type ConnectionTest } from "plsremind";
import { useCallback, useEffect, useRef, useState } from "react";
import { describeError } from "../../hub/errors.js";
import { useHub } from "../../hub/session.js";
import { t } from "../../i18n/index.js";

const pollEvery = 1000;
// The hub gives an app a minute to answer; a little more covers the round trip.
const pollFor = 70_000;

export type TestState =
  | { readonly status: "starting" }
  | { readonly status: "refused"; readonly message: string }
  | ConnectionTest;

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
      while (test.status === "pending" && Date.now() < until && alive.current) {
        await new Promise((resolve) => setTimeout(resolve, pollEvery));
        try {
          test = await client.rest.getConnectionTest(id, test.id);
        } catch {
          break;
        }
        show(test);
      }
      if (alive.current) done();
    },
    [client, done],
  );

  return { state: (id: string): TestState | undefined => states[id], start };
}
