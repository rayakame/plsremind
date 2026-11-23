import { useState } from "react";
import { Link, useSearch } from "wouter";
import { useLoaded } from "../hub/loaded.js";
import { useHub } from "../hub/session.js";
import { useT, type Key } from "../i18n/index.js";
import { routes } from "../shell/router.js";
import { Avatar } from "../ui/avatar.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { rememberLinkOrigin, takeLinkOrigin } from "./linked-origin.js";
import { isRunning, useAppTest } from "./settings/app-test.js";
import { TestLine } from "./settings/apps.js";

const errors: Readonly<Record<string, Key>> = {
  identity_taken: "linked.taken",
  link_disabled: "errors.link_disabled",
  invalid_state: "linked.invalidState",
  provider_error: "errors.provider_error",
};

/** Where the browser lands after linking an account elsewhere: what came of it, and what next. */
export default function LinkedPage({ provider }: { provider: string }) {
  const t = useT();
  const { client, info } = useHub();
  const search = new URLSearchParams(useSearch());
  const [origin] = useState(takeLinkOrigin);
  const identities = useLoaded((signal) => client.rest.listIdentities({ signal }), client);
  const apps = useLoaded((signal) => client.rest.listUserConnections({ signal }), client);
  const tests = useAppTest(apps.reload);

  const name = info.loginMethods.find((method) => method.key === provider)?.name ?? provider;
  const own = (apps.data ?? []).filter((app) => app.provider === provider);
  const back = origin ?? routes.settings(own.length > 0 ? "apps" : "security");
  const error = search.get("error");
  const retry = client.rest.oauthStartUrl(provider, { intent: "link" });

  if (error !== null) {
    return (
      <main className="center">
        <div className="mark">
          <span className="ava l square" aria-hidden="true">
            <Icon name="warn" />
          </span>
          <span className="badge bad" aria-hidden="true">
            <Icon name="close" />
          </span>
        </div>
        <div className="stack">
          <h1 className="h1">{t("linked.failed")}</h1>
          <p className="lead">{t(errors[error] ?? "errors.unexpected", { name })}</p>
        </div>
        <section className="card" aria-label={t("linked.whatNow")}>
          <h2 className="label">{t("linked.whatNow")}</h2>
          {error === "identity_taken" ? (
            <ol className="stack">
              <li>{t("linked.takenElsewhere", { name })}</li>
              <li>{t("linked.takenOther", { name })}</li>
            </ol>
          ) : (
            <p className="text">{t("linked.tryAgain")}</p>
          )}
          <p className="text dim small">{t("linked.unchanged")}</p>
        </section>
        <div className="line">
          <a className="btn primary" href={retry} onClick={() => rememberLinkOrigin(back)}>
            {t("linked.retry")}
          </a>
          <Link href={back} className="btn ghost">
            {t("linked.back")}
          </Link>
        </div>
      </main>
    );
  }

  const identity = identities.data?.find((one) => one.id === search.get("identity"));
  const testable = own.filter(
    (app) => app.enabled && app.remind && app.intents.includes("deliveries"),
  );
  return (
    <main className="center">
      <div className="mark">
        <Avatar
          name={identity?.displayName ?? name}
          src={identity?.hasAvatar ? client.rest.identityAvatarUrl(identity.id) : undefined}
          size="l"
        />
        <span className="badge" aria-hidden="true">
          <Icon name="check" />
        </span>
      </div>
      <div className="stack">
        <h1 className="h1">{t("linked.title", { name })}</h1>
        {identity && (
          <p className="lead">
            {identity.username
              ? t("linked.asHandle", { name: identity.displayName, handle: identity.username })
              : t("linked.as", { name: identity.displayName })}
          </p>
        )}
      </div>
      {own.length > 0 && (
        <section className="card" aria-label={t("linked.next")}>
          <h2 className="label">{t("linked.next")}</h2>
          <p className="text">{t("linked.nextText", { name })}</p>
          <div role="list" className="rows">
            {own.map((app) => {
              const test = tests.state(app.id);
              return (
                <div role="listitem" aria-label={app.name} className="row tall" key={app.id}>
                  <Avatar name={app.name} square />
                  <span className="fill stack0">
                    <span className="strong">{app.name}</span>
                    {!test && app.description && (
                      <span className="dim small">{app.description}</span>
                    )}
                    <span role="status">{test && <TestLine state={test} />}</span>
                  </span>
                  {app.installUrl && (
                    <a className="btn" href={app.installUrl} target="_blank" rel="noreferrer">
                      {t(provider === "discord" ? "apps.addInDiscord" : "apps.setUp")}
                      <Icon name="open" />
                    </a>
                  )}
                </div>
              );
            })}
          </div>
          {testable.length > 0 && (
            <div className="between">
              <span className="dim">{t("linked.testText")}</span>
              <Button
                variant="ghost"
                disabled={testable.some((app) => isRunning(tests.state(app.id)))}
                onClick={() => testable.forEach((app) => void tests.start(app.id))}
              >
                {t("linked.test")}
              </Button>
            </div>
          )}
        </section>
      )}
      <div className="line">
        <Link href={back} className="btn primary">
          {t("common.done")}
        </Link>
        <Link href={routes.items} className="btn ghost">
          {t("linked.home")}
        </Link>
      </div>
    </main>
  );
}
