import { useEffect, useMemo, useState } from "react";
import { nowIn, type Now } from "../model/groups.js";
import { useProfile } from "./hooks.js";
import { readPrefs } from "./prefs.js";

const tick = 20_000;

/** The present for the views. It moves on once a minute and when the tab becomes visible. */
export function useNow(): Now {
  const profile = useProfile();
  const { weekStart } = readPrefs(profile.preferences);
  const [minute, setMinute] = useState(() => Math.floor(Date.now() / 60_000));
  useEffect(() => {
    const update = () => setMinute(Math.floor(Date.now() / 60_000));
    const timer = setInterval(update, tick);
    document.addEventListener("visibilitychange", update);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return useMemo(
    () => nowIn(profile.timezone, weekStart, new Date(Math.max(minute * 60_000, Date.now()))),
    [profile.timezone, weekStart, minute],
  );
}
