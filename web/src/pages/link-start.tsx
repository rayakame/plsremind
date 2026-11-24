import { useEffect } from "react";
import { useHub } from "../hub/session.js";
import { useT } from "../i18n/index.js";
import { leave } from "../shell/leave.js";
import { routes } from "../shell/router.js";
import { rememberLinkOrigin } from "./linked-origin.js";

/** Starts linking an account at a provider, for links from outside the dashboard such as a bot's. */
export function LinkStart({ provider }: { provider: string }) {
  const t = useT();
  const { client, info } = useHub();
  const name = info.loginMethods.find((method) => method.key === provider)?.name ?? provider;
  const start = client.rest.oauthStartUrl(provider, { intent: "link" });
  useEffect(() => {
    rememberLinkOrigin(routes.settings("apps"));
    leave(start);
  }, [start]);
  return (
    <main className="center">
      <p className="lead">{t("link.leaving", { name })}</p>
      <div className="line">
        <a className="btn primary" href={start}>
          {t("link.continue", { name })}
        </a>
      </div>
    </main>
  );
}
