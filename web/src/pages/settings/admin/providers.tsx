import type { LoginProvider, ProviderInput } from "plsremind";
import { useState, type FormEvent } from "react";
import { useLoaded } from "../../../hub/loaded.js";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Button, Field, Input, Toggle } from "../../../ui/controls.js";
import { Card } from "../../../ui/layout.js";
import { ConfirmDialog } from "../../../ui/name-dialog.js";

const builtIn = new Set(["password", "github", "discord"]);

function ProviderCard(props: {
  provider: LoginProvider;
  onChange: (next: LoginProvider) => void;
  onDelete: () => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const { provider } = props;
  const [name, setName] = useState(provider.name);
  const [login, setLogin] = useState(provider.loginEnabled);
  const [link, setLink] = useState(provider.linkEnabled);
  const [clientId, setClientId] = useState(provider.clientId);
  const [secret, setSecret] = useState("");
  const [issuer, setIssuer] = useState(provider.issuerUrl);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const external = provider.type !== "password";

  const save = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setSaved(false);
    const body: ProviderInput = { name, loginEnabled: login, linkEnabled: link };
    if (external) {
      body.clientId = clientId;
      if (secret !== "") body.clientSecret = secret;
      if (provider.type === "oidc") body.issuerUrl = issuer;
    }
    const next = await guard(client.rest.setProvider(provider.key, body));
    setBusy(false);
    if (!next) return;
    setSecret("");
    setSaved(true);
    props.onChange(next);
  };
  const remove = async () => {
    if ((await guard(client.rest.deleteProvider(provider.key).then(() => true))) === true) {
      props.onDelete();
    }
  };

  return (
    <fieldset className="card" aria-label={provider.name}>
      <legend className="label">{provider.key}</legend>
      <form onSubmit={(event) => void save(event)}>
        <fieldset className="stack" disabled={busy}>
          <Field label={t("provider.name")}>
            <Input value={name} onChange={(event) => setName(event.target.value)} required />
          </Field>
          {external && <p className="text dim">{t("provider.forApps")}</p>}
          <Toggle label={t("provider.login")} checked={login} onChange={setLogin} />
          <Toggle label={t("provider.link")} checked={link} onChange={setLink} />
          {external && (
            <>
              <Field label={t("provider.clientId")}>
                <Input
                  value={clientId}
                  autoComplete="off"
                  onChange={(event) => setClientId(event.target.value)}
                />
              </Field>
              <Field label={t("provider.clientSecret")}>
                <Input
                  type="password"
                  autoComplete="new-password"
                  value={secret}
                  placeholder={provider.hasClientSecret ? t("provider.secretStored") : ""}
                  onChange={(event) => setSecret(event.target.value)}
                />
              </Field>
              {provider.type === "oidc" && (
                <Field label={t("provider.issuer")}>
                  <Input
                    type="url"
                    value={issuer}
                    onChange={(event) => setIssuer(event.target.value)}
                  />
                </Field>
              )}
            </>
          )}
          <div className="line">
            <Button variant="primary" type="submit" disabled={busy}>
              {t("common.save")}
            </Button>
            {!builtIn.has(provider.key) && (
              <Button variant="danger" onClick={() => setDeleting(true)}>
                {t("common.delete")}
              </Button>
            )}
            {saved && <span className="dim">{t("provider.saved")}</span>}
          </div>
        </fieldset>
      </form>
      {deleting && (
        <ConfirmDialog
          title={t("provider.delete", { name: provider.name })}
          confirm={t("common.delete")}
          onConfirm={() => void remove()}
          onClose={() => setDeleting(false)}
        />
      )}
    </fieldset>
  );
}

export function AdminProviders() {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const providers = useLoaded((signal) => client.rest.listProviders({ signal }), client);
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);

  const add = async (event: FormEvent) => {
    event.preventDefault();
    const wanted = key.trim();
    if (wanted === "" || busy) return;
    setBusy(true);
    const created = await guard(client.rest.setProvider(wanted, { type: "oidc", name: wanted }));
    setBusy(false);
    if (!created) return;
    setKey("");
    providers.setData((before) => [
      ...(before ?? []).filter((one) => one.key !== created.key),
      created,
    ]);
  };

  return (
    <>
      {providers.data?.map((provider) => (
        <ProviderCard
          key={provider.key}
          provider={provider}
          onChange={(next) =>
            providers.setData((before) => before?.map((one) => (one.key === next.key ? next : one)))
          }
          onDelete={() =>
            providers.setData((before) => before?.filter((one) => one.key !== provider.key))
          }
        />
      ))}
      <Card title={t("provider.add")}>
        <p className="text dim">{t("provider.addText")}</p>
        <form className="stack" onSubmit={(event) => void add(event)}>
          <Field label={t("provider.newKey")}>
            <Input
              value={key}
              autoCapitalize="none"
              onChange={(event) => setKey(event.target.value)}
              required
              disabled={busy}
            />
          </Field>
          <div className="line">
            <Button variant="primary" type="submit" disabled={busy}>
              {t("provider.add")}
            </Button>
          </div>
        </form>
      </Card>
    </>
  );
}
