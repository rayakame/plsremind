import type { Identity, UserConnection } from "plsremind";
import { useState } from "react";
import { useLoaded, type Loaded } from "../../hub/loaded.js";
import { useNow } from "../../hub/now.js";
import { useHub } from "../../hub/session.js";
import { useT, type Key } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { routes } from "../../shell/router.js";
import { Avatar } from "../../ui/avatar.js";
import { Button, Segmented } from "../../ui/controls.js";
import { Icon } from "../../ui/icon.js";
import { LoadFailed } from "../../ui/load-failed.js";
import { formatMoment } from "../item/labels.js";
import { rememberLinkOrigin } from "../linked-origin.js";
import { isRunning, useAppTest, type TestState } from "./app-test.js";
import {
  accessOf,
  grantFor,
  levelsOf,
  sectionsOf,
  statusOf,
  type Access,
  type Section,
} from "./apps-model.js";
import { IdentityRow, UnlinkDialog } from "./identity.js";

const accessWords: Record<"discord" | "other", Record<Access, Key>> = {
  discord: { none: "apps.none", use: "apps.commands", remind: "apps.commandsReminders" },
  other: { none: "apps.none", use: "apps.access", remind: "apps.accessReminders" },
};

/** How a test of an app is going, in one line. */
export function TestLine({ state }: { state: TestState }) {
  const t = useT();
  if (state.status === "refused") return <div className="state bad">{state.message}</div>;
  if (state.status === "lost") return <div className="state">{t("apps.testLost")}</div>;
  if (state.status === "starting" || state.status === "pending") {
    return <div className="state">{t("apps.testRunning")}</div>;
  }
  if (state.status === "delivered") {
    return (
      <div className="state">
        <Icon name="check" />
        {t("apps.testDelivered")}
      </div>
    );
  }
  if (state.status === "failed") {
    return (
      <div className="state bad">
        <Icon name="warn" />
        {t("apps.testFailed", { reason: state.reason })}
      </div>
    );
  }
  return (
    <div className="state">
      {t(state.reason === "not_connected" ? "apps.testNotConnected" : "apps.testNoAnswer")}
    </div>
  );
}

function StatusLine({ app }: { app: UserConnection }) {
  const t = useT();
  const now = useNow();
  const status = statusOf(app);
  const text = t(status.key, {
    when: status.at ? formatMoment(status.at, now) : "",
    reason: status.reason ?? "",
  });
  if (status.tone === "bad") {
    return (
      <div className="state bad">
        <Icon name="warn" />
        <span>
          {text} {t("apps.failedHint")}
        </span>
      </div>
    );
  }
  return (
    <div className="state">
      {status.tone === "ok" && <Icon name="check" />}
      {text}
    </div>
  );
}

function AppCard(props: {
  app: UserConnection;
  section: Section;
  test: TestState | undefined;
  onTest: () => void;
  onChange: (next: UserConnection) => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const [busy, setBusy] = useState(false);
  const { app, test } = props;
  const discord = props.section.provider === "discord";
  const words = accessWords[discord ? "discord" : "other"];
  const failed = app.lastDelivery?.status === "failed";
  const running = isRunning(test);
  const canTest = app.linked && app.enabled && app.intents.includes("deliveries");
  const sees = app.intents
    .filter((intent) => intent !== "deliveries")
    .map((intent) => t(`intent.${intent}`))
    .join(", ");

  const choose = async (access: Access) => {
    setBusy(true);
    const next = await guard(client.rest.setConnectionGrant(app.id, grantFor(access)));
    setBusy(false);
    if (next) props.onChange(next);
  };

  return (
    <section className="card" aria-label={app.name}>
      <div className="who">
        <Avatar name={app.name} square />
        <span className="fill stack0">
          <span className="strong">{app.name}</span>
          {app.description && <span className="dim">{app.description}</span>}
        </span>
        {app.installUrl && (
          <a
            className={failed ? "btn primary" : "btn ghost"}
            href={app.installUrl}
            target="_blank"
            rel="noreferrer"
          >
            {t(discord ? "apps.addInDiscord" : "apps.setUp")}
            <Icon name="open" />
          </a>
        )}
      </div>
      <div className="between">
        {test ? <TestLine state={test} /> : <StatusLine app={app} />}
        {canTest && (
          <Button variant={failed ? undefined : "ghost"} disabled={running} onClick={props.onTest}>
            {t("apps.test")}
          </Button>
        )}
      </div>
      <div className="between">
        <span>{t("apps.may")}</span>
        <Segmented<Access>
          label={t("apps.mayOf", { name: app.name })}
          value={accessOf(app)}
          disabled={busy || !app.linked}
          options={levelsOf(app).map((level) => ({ value: level, label: t(words[level]) }))}
          onChange={(access) => void choose(access)}
        />
      </div>
      {!app.linked && (
        <p className="text dim small">{t("apps.linkFirst", { name: props.section.name })}</p>
      )}
      {sees && <p className="text dim small">{t("apps.sees", { what: sees })}</p>}
    </section>
  );
}

function AccountCard(props: {
  section: Section;
  identities: Loaded<Identity[]>;
  onUnlink: (identity: Identity) => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { section } = props;
  const link = (label: Key, primary: boolean) => (
    <a
      className={primary ? "btn primary" : "btn"}
      href={client.rest.oauthStartUrl(section.provider, { intent: "link" })}
      onClick={() => rememberLinkOrigin(routes.settings("apps"))}
    >
      {t(label, { name: section.name })}
    </a>
  );
  const title = t("apps.account", { name: section.name });
  const mine = props.identities.data?.filter((one) => one.provider === section.provider);
  return (
    <section className="card" aria-label={title}>
      <h3 className="label">{title}</h3>
      {props.identities.failed && <LoadFailed onRetry={props.identities.reload} />}
      {mine === undefined ? null : mine.length > 0 ? (
        <>
          <div role="list" className="stack">
            {mine.map((identity) => (
              <IdentityRow
                key={identity.id}
                identity={identity}
                provider={section.name}
                onUnlink={() => props.onUnlink(identity)}
              />
            ))}
          </div>
          <p className="text dim">
            {t(section.login ? "apps.accountText" : "apps.accountTextLinkOnly", {
              name: section.name,
            })}
          </p>
          {section.linkable && <div className="line">{link("apps.linkAnother", false)}</div>}
        </>
      ) : (
        <div className="who">
          <span className="fill stack0">
            <span className="strong">{t("apps.notLinked")}</span>
            <span className="dim">
              {t(section.linkable ? "apps.notLinkedText" : "apps.cannotLink", {
                name: section.name,
              })}
            </span>
          </span>
          {section.linkable && link("apps.link", true)}
        </div>
      )}
    </section>
  );
}

export function AppsSettings({ apps }: { apps: Loaded<UserConnection[]> }) {
  const t = useT();
  const { client, info } = useHub();
  const { guard } = useNotices();
  const identities = useLoaded((signal) => client.rest.listIdentities({ signal }), client);
  const tests = useAppTest(apps.reload);
  const [unlinking, setUnlinking] = useState<Identity>();
  const sections = sectionsOf(apps.data ?? [], info.loginMethods);

  const unlink = async (identity: Identity) => {
    setUnlinking(undefined);
    if ((await guard(client.rest.unlinkIdentity(identity.id).then(() => true))) === true) {
      identities.setData((before) => before?.filter((one) => one.id !== identity.id));
      apps.reload();
    }
  };
  const replace = (next: UserConnection) =>
    apps.setData((before) => before?.map((one) => (one.id === next.id ? next : one)));

  return (
    <>
      <p className="lead">{t("apps.lead")}</p>
      {apps.failed && <LoadFailed onRetry={apps.reload} />}
      {apps.data?.length === 0 && <p className="text dim">{t("apps.empty")}</p>}
      {sections.map((section) => (
        <section key={section.provider} className="stack" aria-label={section.name}>
          <h2 className="h2">{section.name}</h2>
          <AccountCard section={section} identities={identities} onUnlink={setUnlinking} />
          {section.apps.map((app) => (
            <AppCard
              key={app.id}
              app={app}
              section={section}
              test={tests.state(app.id)}
              onTest={() => void tests.start(app.id)}
              onChange={replace}
            />
          ))}
        </section>
      ))}
      {unlinking && (
        <UnlinkDialog
          provider={
            info.loginMethods.find((method) => method.key === unlinking.provider)?.name ??
            unlinking.provider
          }
          onConfirm={() => void unlink(unlinking)}
          onClose={() => setUnlinking(undefined)}
        />
      )}
    </>
  );
}
