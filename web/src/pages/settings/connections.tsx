import type { Identity, UserConnection } from "plsremind";
import { useState } from "react";
import { useLoaded } from "../../hub/loaded.js";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Button, Toggle } from "../../ui/controls.js";
import { Card } from "../../ui/layout.js";
import { ConfirmDialog } from "../../ui/name-dialog.js";

function ConnectionCard(props: {
  connection: UserConnection;
  onChange: (next: UserConnection) => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const [busy, setBusy] = useState(false);
  const { connection } = props;

  const grant = async (change: { enabled?: boolean; remind?: boolean }) => {
    setBusy(true);
    const next = await guard(client.rest.setConnectionGrant(connection.id, change));
    setBusy(false);
    if (next) props.onChange(next);
  };
  const receives = connection.intents.map((intent) => t(`intent.${intent}`)).join(", ");

  return (
    <fieldset className="card" aria-label={connection.name}>
      <legend className="label">{connection.provider}</legend>
      <h3 className="h2">{connection.name}</h3>
      {connection.description && <p className="text">{connection.description}</p>}
      <p className="text dim">{t("conn.receives", { what: receives })}</p>
      {!connection.linked && (
        <a
          className="btn"
          href={client.rest.oauthStartUrl(connection.provider, { intent: "link" })}
        >
          {t("conn.link", { provider: connection.provider })}
        </a>
      )}
      <Toggle
        label={t("conn.use")}
        checked={connection.enabled}
        disabled={busy || !connection.linked}
        onChange={(enabled) => void grant({ enabled })}
      />
      <Toggle
        label={t("conn.remind")}
        checked={connection.remind}
        disabled={busy || !connection.enabled}
        onChange={(remind) => void grant({ remind })}
      />
    </fieldset>
  );
}

export function ConnectionSettings() {
  const t = useT();
  const { client, info } = useHub();
  const { guard } = useNotices();
  const connections = useLoaded((signal) => client.rest.listUserConnections({ signal }), client);
  const identities = useLoaded((signal) => client.rest.listIdentities({ signal }), client);
  const [unlinking, setUnlinking] = useState<Identity>();

  const nameOf = (provider: string) =>
    provider === "password"
      ? t("account.password")
      : (info.loginMethods.find((method) => method.key === provider)?.name ?? provider);
  const linked = new Set(identities.data?.map((identity) => identity.provider));
  const linkable = info.loginMethods.filter(
    (method) => method.type !== "password" && !linked.has(method.key),
  );
  const unlink = async (identity: Identity) => {
    if ((await guard(client.rest.unlinkIdentity(identity.id).then(() => true))) === true) {
      identities.setData((before) => before?.filter((one) => one.id !== identity.id));
    }
  };

  return (
    <>
      {connections.data?.length === 0 && <p className="lead">{t("conn.none")}</p>}
      {connections.data?.map((connection) => (
        <ConnectionCard
          key={connection.id}
          connection={connection}
          onChange={(next) =>
            connections.setData((before) => before?.map((one) => (one.id === next.id ? next : one)))
          }
        />
      ))}
      <Card title={t("accounts.title")}>
        <div role="list" className="stack">
          {identities.data?.map((identity) => (
            <div role="listitem" className="between" key={identity.id}>
              <span className="kv fill">
                <span>{nameOf(identity.provider)}</span>
                <span>{identity.displayName}</span>
              </span>
              {identity.provider !== "password" && (
                <Button onClick={() => setUnlinking(identity)}>{t("accounts.unlink")}</Button>
              )}
            </div>
          ))}
        </div>
        {linkable.map((method) => (
          <a
            key={method.key}
            className="btn"
            href={client.rest.oauthStartUrl(method.key, { intent: "link" })}
          >
            {t("accounts.link", { name: method.name })}
          </a>
        ))}
      </Card>
      {unlinking && (
        <ConfirmDialog
          title={`${t("accounts.unlink")}: ${nameOf(unlinking.provider)}`}
          text={t("accounts.unlinkText")}
          confirm={t("accounts.unlink")}
          onConfirm={() => void unlink(unlinking)}
          onClose={() => setUnlinking(undefined)}
        />
      )}
    </>
  );
}
