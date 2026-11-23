import type { Identity } from "plsremind";
import { useState, type FormEvent } from "react";
import { describeError, isSignedOut } from "../../hub/errors.js";
import { useProfile } from "../../hub/hooks.js";
import { useLoaded } from "../../hub/loaded.js";
import { useHub, useSessionStore } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { useNotices } from "../../shell/notices.js";
import { Button, Field, Input } from "../../ui/controls.js";
import { routes } from "../../shell/router.js";
import { Dialog } from "../../ui/dialog.js";
import { Card, Notice } from "../../ui/layout.js";
import { LoadFailed } from "../../ui/load-failed.js";
import { ConfirmDialog } from "../../ui/name-dialog.js";
import { rememberLinkOrigin } from "../linked-origin.js";
import { IdentityRow } from "./identity.js";

function SignInMethods() {
  const t = useT();
  const { client, info } = useHub();
  const { guard } = useNotices();
  const identities = useLoaded((signal) => client.rest.listIdentities({ signal }), client);
  const [unlinking, setUnlinking] = useState<Identity>();
  const nameOf = (provider: string) =>
    info.loginMethods.find((method) => method.key === provider)?.name ?? provider;
  const others = identities.data?.filter((identity) => identity.provider !== "password") ?? [];
  const linked = new Set(identities.data?.map((identity) => identity.provider));
  const linkable = info.loginMethods.filter(
    (method) => method.type !== "password" && method.link && !linked.has(method.key),
  );
  const unlink = async (identity: Identity) => {
    setUnlinking(undefined);
    if ((await guard(client.rest.unlinkIdentity(identity.id).then(() => true))) === true) {
      identities.setData((before) => before?.filter((one) => one.id !== identity.id));
    }
  };

  return (
    <Card title={t("security.methods")}>
      {identities.failed && <LoadFailed onRetry={identities.reload} />}
      <div role="list" className="stack" aria-label={t("security.methods")}>
        {others.map((identity) => (
          <IdentityRow
            key={identity.id}
            identity={identity}
            provider={nameOf(identity.provider)}
            onUnlink={() => setUnlinking(identity)}
          />
        ))}
      </div>
      {identities.data && others.length === 0 && (
        <p className="text dim">{t("security.noMethods")}</p>
      )}
      {linkable.length > 0 && (
        <div className="line">
          {linkable.map((method) => (
            <a
              key={method.key}
              className="btn"
              href={client.rest.oauthStartUrl(method.key, { intent: "link" })}
              onClick={() => rememberLinkOrigin(routes.settings("security"))}
            >
              {t("accounts.link", { name: method.name })}
            </a>
          ))}
        </div>
      )}
      {unlinking && (
        <ConfirmDialog
          title={`${t("accounts.unlink")}: ${nameOf(unlinking.provider)}`}
          text={t("accounts.unlinkText")}
          confirm={t("accounts.unlink")}
          onConfirm={() => void unlink(unlinking)}
          onClose={() => setUnlinking(undefined)}
        />
      )}
    </Card>
  );
}

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
  if (!identities.data) {
    return (
      <Card title={t("pw.title")}>
        {identities.failed && <LoadFailed onRetry={identities.reload} />}
      </Card>
    );
  }
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

export function SecuritySettings() {
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
      <SignInMethods />
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
