import type { Connection, Intent } from "plsremind";
import { useState, type FormEvent } from "react";
import { useLoaded } from "../../../hub/loaded.js";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Button, Field, Input, Toggle } from "../../../ui/controls.js";
import { Card } from "../../../ui/layout.js";
import { ConfirmDialog } from "../../../ui/name-dialog.js";
import { SecretDialog } from "../tokens.js";

const intents: readonly Intent[] = ["users", "lists", "items", "plans", "history", "deliveries"];

function Intents(props: {
  value: readonly Intent[];
  disabled?: boolean;
  onChange: (next: Intent[]) => void;
}) {
  const t = useT();
  return (
    <fieldset className="stack" aria-label={t("aconn.may")}>
      <legend className="label">{t("aconn.may")}</legend>
      {intents.map((intent) => (
        <Toggle
          key={intent}
          label={t(`intent.${intent}`)}
          checked={props.value.includes(intent)}
          disabled={props.disabled}
          onChange={(on) =>
            props.onChange(
              intents.filter((one) => (one === intent ? on : props.value.includes(one))),
            )
          }
        />
      ))}
    </fieldset>
  );
}

function ConnectionCard(props: {
  connection: Connection;
  onChange: (next: Connection) => void;
  onDelete: () => void;
  onToken: (token: string) => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const [dialog, setDialog] = useState<"rotate" | "delete">();
  const [busy, setBusy] = useState(false);
  const { connection } = props;

  const change = async (changes: { enabled?: boolean; allowedIntents?: readonly Intent[] }) => {
    setBusy(true);
    const next = await guard(client.rest.updateConnection(connection.id, changes));
    setBusy(false);
    if (next) props.onChange(next);
  };
  const rotate = async () => {
    if (busy) return;
    setBusy(true);
    const next = await guard(client.rest.rotateConnectionToken(connection.id));
    setBusy(false);
    if (next?.token) props.onToken(next.token);
  };
  const remove = async () => {
    if ((await guard(client.rest.deleteConnection(connection.id).then(() => true))) === true) {
      props.onDelete();
    }
  };

  return (
    <fieldset className="card" aria-label={connection.name}>
      <legend className="label">{connection.provider}</legend>
      <h3 className="h2">{connection.name}</h3>
      {connection.description && <p className="text">{connection.description}</p>}
      <Toggle
        label={t("aconn.enabled")}
        checked={connection.enabled}
        disabled={busy}
        onChange={(enabled) => void change({ enabled })}
      />
      <Intents
        value={connection.allowedIntents}
        disabled={busy}
        onChange={(allowedIntents) => void change({ allowedIntents })}
      />
      <div className="line">
        <Button disabled={busy} onClick={() => setDialog("rotate")}>
          {t("aconn.newToken")}
        </Button>
        <Button variant="danger" onClick={() => setDialog("delete")}>
          {t("common.delete")}
        </Button>
      </div>
      {dialog === "rotate" && (
        <ConfirmDialog
          title={t("aconn.newToken")}
          text={t("aconn.newTokenText")}
          confirm={t("aconn.newToken")}
          onConfirm={() => void rotate()}
          onClose={() => setDialog(undefined)}
        />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("aconn.delete", { name: connection.name })}
          text={t("aconn.deleteText")}
          confirm={t("common.delete")}
          onConfirm={() => void remove()}
          onClose={() => setDialog(undefined)}
        />
      )}
    </fieldset>
  );
}

export function AdminConnections() {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const connections = useLoaded((signal) => client.rest.listConnections({ signal }), client);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("");
  const [description, setDescription] = useState("");
  const [allowed, setAllowed] = useState<Intent[]>([]);
  const [token, setToken] = useState<string>();
  const [busy, setBusy] = useState(false);

  const install = async (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    const created = await guard(
      client.rest.createConnection({
        name: name.trim(),
        provider: provider.trim(),
        ...(description.trim() ? { description: description.trim() } : {}),
        allowedIntents: allowed,
      }),
    );
    setBusy(false);
    if (!created) return;
    const { token: secret, ...kept } = created;
    setToken(secret);
    setName("");
    setProvider("");
    setDescription("");
    setAllowed([]);
    connections.setData((before) => [...(before ?? []), kept]);
  };

  return (
    <>
      {connections.data?.length === 0 && <p className="text dim">{t("aconn.none")}</p>}
      {connections.data?.map((connection) => (
        <ConnectionCard
          key={connection.id}
          connection={connection}
          onChange={({ token: _secret, ...next }) =>
            connections.setData((before) => before?.map((one) => (one.id === next.id ? next : one)))
          }
          onDelete={() =>
            connections.setData((before) => before?.filter((one) => one.id !== connection.id))
          }
          onToken={setToken}
        />
      ))}
      <Card title={t("aconn.install")}>
        <form className="stack" onSubmit={(event) => void install(event)}>
          <Field label={t("common.name")}>
            <Input value={name} onChange={(event) => setName(event.target.value)} required />
          </Field>
          <Field label={t("aconn.provider")}>
            <Input
              value={provider}
              autoCapitalize="none"
              onChange={(event) => setProvider(event.target.value)}
              required
            />
          </Field>
          <Field label={t("aconn.description")}>
            <Input value={description} onChange={(event) => setDescription(event.target.value)} />
          </Field>
          <Intents value={allowed} onChange={setAllowed} />
          <div className="line">
            <Button variant="primary" type="submit" disabled={busy}>
              {t("aconn.installDo")}
            </Button>
          </div>
        </form>
      </Card>
      {token !== undefined && (
        <SecretDialog
          title={t("aconn.token")}
          label={t("tokens.value")}
          value={token}
          onClose={() => setToken(undefined)}
        />
      )}
    </>
  );
}
