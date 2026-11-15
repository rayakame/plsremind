import { useState, type FormEvent } from "react";
import { describeError, isSignedOut } from "../../hub/errors.js";
import { useProfile } from "../../hub/hooks.js";
import { useLoaded } from "../../hub/loaded.js";
import { useHub, useSessionStore } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Button, Field, Input } from "../../ui/controls.js";
import { Dialog } from "../../ui/dialog.js";
import { Card, Notice } from "../../ui/layout.js";

function PasswordCard() {
  const t = useT();
  const { client } = useHub();
  const session = useSessionStore();
  const identities = useLoaded((signal) => client.rest.listIdentities({ signal }), client);
  const [current, setCurrent] = useState("");
  const [username, setUsername] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [changed, setChanged] = useState(false);
  if (!identities.data) return <Card title={t("pw.title")}>{null}</Card>;
  const has = identities.data.some((identity) => identity.provider === "password");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    setChanged(false);
    try {
      await client.rest.setPassword(
        has ? { currentPassword: current, newPassword: next } : { username, newPassword: next },
      );
      setCurrent("");
      setNext("");
      setChanged(true);
      identities.reload();
    } catch (failure) {
      if (isSignedOut(failure)) session.reload();
      else setError(describeError(failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title={t("pw.title")}>
      <p className="text dim">{t(has ? "pw.text" : "pw.setText")}</p>
      {error && (
        <Notice tone="danger" flat>
          {error}
        </Notice>
      )}
      {changed && <Notice flat>{t("pw.changed")}</Notice>}
      <form onSubmit={(event) => void submit(event)}>
        <fieldset className="stack" disabled={busy}>
          {has ? (
            <Field label={t("pw.current")}>
              <Input
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(event) => setCurrent(event.target.value)}
                required
              />
            </Field>
          ) : (
            <Field label={t("account.username")}>
              <Input
                autoComplete="username"
                autoCapitalize="none"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </Field>
          )}
          <Field label={t("pw.new")}>
            <Input
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(event) => setNext(event.target.value)}
              required
            />
          </Field>
          <div className="line">
            <Button variant="primary" type="submit" disabled={busy}>
              {t(has ? "pw.change" : "pw.set")}
            </Button>
          </div>
        </fieldset>
      </form>
    </Card>
  );
}

export function LoginSettings() {
  const t = useT();
  const { client } = useHub();
  const session = useSessionStore();
  const { guard } = useNotices();
  const profile = useProfile();
  const [deleting, setDeleting] = useState(false);
  const [typed, setTyped] = useState("");

  const remove = async () => {
    if ((await guard(client.rest.deleteUser().then(() => true))) === true) session.reload();
  };

  return (
    <>
      <PasswordCard />
      <Card title={t("account.deleteTitle")}>
        <p className="text dim">{t("account.deleteText")}</p>
        <div className="line">
          <Button variant="danger" onClick={() => setDeleting(true)}>
            {t("account.deleteOpen")}
          </Button>
        </div>
      </Card>
      {deleting && (
        <Dialog title={t("account.deleteTitle")} onClose={() => setDeleting(false)}>
          <p className="text">{t("account.deleteText")}</p>
          <Field label={t("account.deleteConfirm", { name: profile.displayName })}>
            <Input value={typed} onChange={(event) => setTyped(event.target.value)} />
          </Field>
          <div className="line">
            <Button
              variant="danger"
              disabled={typed !== profile.displayName}
              onClick={() => void remove()}
            >
              {t("account.deleteForGood")}
            </Button>
            <Button variant="ghost" onClick={() => setDeleting(false)}>
              {t("common.cancel")}
            </Button>
          </div>
        </Dialog>
      )}
    </>
  );
}
