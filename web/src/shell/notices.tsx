import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { describeError, isSignedOut } from "../hub/errors.js";
import { useSessionStore } from "../hub/session.js";
import { useT } from "../i18n/index.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { Notice } from "../ui/layout.js";

interface Shown {
  id: number;
  message: string;
  tone: "danger" | undefined;
}

interface Notices {
  notify: (message: string, tone?: "danger") => void;
  /** Runs a call and tells the user if it fails. Resolves with undefined in that case. */
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
}

const NoticesContext = createContext<Notices | undefined>(undefined);
const showFor = 6000;

export function NoticesProvider(props: { children: ReactNode }) {
  const t = useT();
  const session = useSessionStore();
  const [shown, setShown] = useState<readonly Shown[]>([]);
  const next = useRef(0);

  const close = useCallback((id: number) => {
    setShown((all) => all.filter((one) => one.id !== id));
  }, []);

  const notify = useCallback(
    (message: string, tone?: "danger") => {
      const id = next.current++;
      setShown((all) => [...all.slice(-2), { id, message, tone }]);
      setTimeout(() => close(id), showFor);
    },
    [close],
  );

  const guard = useCallback(
    async <T,>(work: Promise<T>): Promise<T | undefined> => {
      try {
        return await work;
      } catch (error) {
        if (isSignedOut(error)) session.reload();
        else notify(describeError(error), "danger");
        return undefined;
      }
    },
    [notify, session],
  );

  const value = useMemo(() => ({ notify, guard }), [notify, guard]);

  return (
    <NoticesContext.Provider value={value}>
      {props.children}
      <div className="notices">
        {shown.map((one) => (
          <Notice
            key={one.id}
            tone={one.tone}
            action={
              <Button variant="icon" aria-label={t("common.close")} onClick={() => close(one.id)}>
                <Icon name="close" />
              </Button>
            }
          >
            {one.message}
          </Notice>
        ))}
      </div>
    </NoticesContext.Provider>
  );
}

export function useNotices(): Notices {
  const notices = useContext(NoticesContext);
  if (!notices) throw new Error("no notices");
  return notices;
}
