import type { PersonalToken } from "plsremind";
import { useState, type FormEvent } from "react";
import { useLoaded } from "../../hub/loaded.js";
import { useNow } from "../../hub/now.js";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Button, Field, Input, Toggle } from "../../ui/controls.js";
import { Dialog } from "../../ui/dialog.js";
import { Card } from "../../ui/layout.js";
import { ConfirmDialog, NameDialog } from "../../ui/name-dialog.js";
import { formatMoment } from "../item/labels.js";

/** A secret that is shown once, with a way to copy it. */
export function SecretDialog(props: {
  title: string;
  label: string;
  value: string;
  onClose: () => void;
}) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(props.value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Dialog title={props.title} onClose={props.onClose} sticky>
      <p className="text">{t("tokens.once")}</p>
      <Field label={props.label}>
        <Input
          className="mono"
          readOnly
          value={props.value}
          onFocus={(event) => event.target.select()}
        />
      </Field>
      <div className="line">
        <Button onClick={() => void copy()}>{t(copied ? "common.copied" : "common.copy")}</Button>
        <Button variant="primary" onClick={props.onClose}>
          {t("common.done")}
        </Button>
      </div>
    </Dialog>
  );
}

function TokenCard(props: {
  token: PersonalToken;
  onChange: (next: PersonalToken) => void;
  onDelete: () => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const now = useNow();
  const [dialog, setDialog] = useState<"rename" | "delete">();
  const [busy, setBusy] = useState(false);
  const { token } = props;

  const change = async (changes: { name?: string; remind?: boolean }) => {
    setBusy(true);
    const next = await guard(client.rest.updateToken(token.id, changes));
    setBusy(false);
    if (next) props.onChange(next);
  };
  const remove = async () => {
    if ((await guard(client.rest.deleteToken(token.id).then(() => true))) === true)
      props.onDelete();
  };

  return (
    <fieldset className="card" aria-label={token.name}>
      <div className="between">
        <h3 className="h2">{token.name}</h3>
        <div className="line">
          <Button onClick={() => setDialog("rename")}>{t("common.rename")}</Button>
          <Button variant="danger" onClick={() => setDialog("delete")}>
            {t("common.delete")}
          </Button>
        </div>
      </div>
      <p className="text dim">
        {token.lastUsedAt
          ? t("tokens.lastUsed", { time: formatMoment(token.lastUsedAt, now) })
          : t("tokens.neverUsed")}
      </p>
      <Toggle
        label={t("conn.remind")}
        checked={token.remind}
        disabled={busy}
        onChange={(remind) => void change({ remind })}
      />
      {dialog === "rename" && (
        <NameDialog
          title={t("common.rename")}
          initial={token.name}
          submit={t("common.save")}
          onSubmit={(name) => void change({ name })}
          onClose={() => setDialog(undefined)}
        />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("tokens.delete")}
          text={t("tokens.deleteText")}
          confirm={t("common.delete")}
          onConfirm={() => void remove()}
          onClose={() => setDialog(undefined)}
        />
      )}
    </fieldset>
  );
}

export function TokenSettings() {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const tokens = useLoaded((signal) => client.rest.listTokens({ signal }), client);
  const [name, setName] = useState("");
  const [remind, setRemind] = useState(false);
  const [busy, setBusy] = useState(false);
  const [secret, setSecret] = useState<string>();

  const create = async (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed === "") return;
    setBusy(true);
    const created = await guard(client.rest.createToken({ name: trimmed, remind }));
    setBusy(false);
    if (!created) return;
    const { token, ...kept } = created;
    setSecret(token);
    setName("");
    setRemind(false);
    tokens.setData((before) => [...(before ?? []), kept]);
  };

  return (
    <>
      <p className="lead">{t("tokens.text")}</p>
      {tokens.data?.length === 0 && <p className="text dim">{t("tokens.none")}</p>}
      {tokens.data?.map((token) => (
        <TokenCard
          key={token.id}
          token={token}
          onChange={(next) =>
            tokens.setData((before) => before?.map((one) => (one.id === next.id ? next : one)))
          }
          onDelete={() => tokens.setData((before) => before?.filter((one) => one.id !== token.id))}
        />
      ))}
      <Card title={t("tokens.create")}>
        <form className="stack" onSubmit={(event) => void create(event)}>
          <Field label={t("tokens.newName")}>
            <Input value={name} onChange={(event) => setName(event.target.value)} required />
          </Field>
          <Toggle label={t("tokens.newRemind")} checked={remind} onChange={setRemind} />
          <div className="line">
            <Button variant="primary" type="submit" disabled={busy}>
              {t("tokens.create")}
            </Button>
          </div>
        </form>
      </Card>
      {secret !== undefined && (
        <SecretDialog
          title={t("tokens.created")}
          label={t("tokens.value")}
          value={secret}
          onClose={() => setSecret(undefined)}
        />
      )}
    </>
  );
}
