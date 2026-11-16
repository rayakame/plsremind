import { useState } from "react";
import { useConnection, useProfile } from "../hub/hooks.js";
import { useHub } from "../hub/session.js";
import { updateProfile } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { Button } from "../ui/controls.js";
import { Notice } from "../ui/layout.js";
import { useNotices } from "./notices.js";

export function ConnectionLine() {
  const t = useT();
  const status = useConnection();
  if (status === "connected") return null;
  return <Notice flat>{t("connection.reconnecting")}</Notice>;
}

const dismissedKey = "plsremind.zone-dismissed";

function wasDismissed(zone: string): boolean {
  try {
    return sessionStorage.getItem(dismissedKey) === zone;
  } catch {
    return false;
  }
}

/** Offers to move the profile to the browser's time zone when the two differ. */
export function ZoneLine() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const profile = useProfile();
  const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const [dismissed, setDismissed] = useState(() => wasDismissed(browser));
  if (!browser || browser === profile.timezone || dismissed) return null;

  const dismiss = () => {
    setDismissed(true);
    try {
      sessionStorage.setItem(dismissedKey, browser);
    } catch {
      // The line then returns on the next load.
    }
  };
  const adopt = () => guard(updateProfile(hub, { timezone: browser }));

  return (
    <Notice
      flat
      action={
        <span className="line">
          <Button onClick={() => void adopt()}>{t("zone.use", { zone: browser })}</Button>
          <Button variant="ghost" onClick={dismiss}>
            {t("zone.keep")}
          </Button>
        </span>
      }
    >
      {t("zone.differs", { zone: profile.timezone })}
    </Notice>
  );
}
