import type { AdminUser, Role } from "plsremind";
import { useEffect, useState, type FormEvent } from "react";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Button, Field, Input, Select, Toggle } from "../../../ui/controls.js";
import { Dialog } from "../../../ui/dialog.js";
import { ConfirmDialog } from "../../../ui/name-dialog.js";

function PasswordDialog(props: { user: AdminUser; onClose: () => void }) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    const done = await guard(
      client.rest.resetPassword(props.user.id, { newPassword: password }).then(() => true),
    );
    setBusy(false);
    if (done) props.onClose();
  };
  return (
    <Dialog title={t("admin.newPassword")} onClose={props.onClose} sticky={busy}>
      <p className="text">{t("admin.newPasswordText", { name: props.user.displayName })}</p>
      <form className="stack" onSubmit={(event) => void submit(event)}>
        <Field label={t("admin.newPassword")}>
          <Input
            type="password"
            autoComplete="new-password"
            disabled={busy}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
        </Field>
        <div className="line">
          <Button variant="primary" type="submit" disabled={busy}>
            {t("common.save")}
          </Button>
          <Button variant="ghost" disabled={busy} onClick={props.onClose}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function UserCard(props: {
  user: AdminUser;
  own: boolean;
  onChange: (next: AdminUser) => void;
  onDelete: () => void;
}) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const [dialog, setDialog] = useState<"password" | "delete">();
  const [busy, setBusy] = useState(false);
  const { user } = props;

  const change = async (changes: { role?: Role; disabled?: boolean }) => {
    setBusy(true);
    const next = await guard(client.rest.updateUserAccess(user.id, changes));
    setBusy(false);
    if (next) props.onChange(next);
  };
  const remove = async () => {
    if ((await guard(client.rest.adminDeleteUser(user.id).then(() => true))) === true) {
      props.onDelete();
    }
  };

  return (
    <fieldset className="card" aria-label={user.displayName}>
      <div className="between">
        <h3 className="h2">{user.displayName}</h3>
        {props.own && <span className="tag">{t("admin.you")}</span>}
      </div>
      <Field label={t("admin.role")}>
        <Select
          value={user.role}
          disabled={busy}
          onChange={(event) => void change({ role: event.target.value as Role })}
        >
          <option value="user">{t("admin.roleUser")}</option>
          <option value="admin">{t("admin.roleAdmin")}</option>
        </Select>
      </Field>
      <Toggle
        label={t("admin.blocked")}
        checked={user.disabled}
        disabled={busy}
        onChange={(disabled) => void change({ disabled })}
      />
      <div className="line">
        <Button onClick={() => setDialog("password")}>{t("admin.newPassword")}</Button>
        <Button variant="danger" onClick={() => setDialog("delete")}>
          {t("common.delete")}
        </Button>
      </div>
      {dialog === "password" && <PasswordDialog user={user} onClose={() => setDialog(undefined)} />}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("admin.deleteUser", { name: user.displayName })}
          text={t("admin.deleteUserText")}
          confirm={t("common.delete")}
          onConfirm={() => void remove()}
          onClose={() => setDialog(undefined)}
        />
      )}
    </fieldset>
  );
}

export function AdminUsers() {
  const t = useT();
  const { client, userId } = useHub();
  const { guard } = useNotices();
  const [users, setUsers] = useState<readonly AdminUser[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const abort = new AbortController();
    client.rest.listUsers({ limit: 50 }, { signal: abort.signal }).then(
      (page) => {
        setUsers(page.data);
        setCursor(page.nextCursor);
      },
      (error: unknown) => {
        if (!abort.signal.aborted) void guard(Promise.reject(error));
      },
    );
    return () => abort.abort();
  }, [client, guard]);

  const more = async () => {
    if (cursor === null || loading) return;
    setLoading(true);
    const page = await guard(client.rest.listUsers({ limit: 50, cursor }));
    setLoading(false);
    if (!page) return;
    setUsers((before) => [...before, ...page.data]);
    setCursor(page.nextCursor);
  };

  return (
    <>
      {users.map((user) => (
        <UserCard
          key={user.id}
          user={user}
          own={user.id === userId}
          onChange={(next) =>
            setUsers((before) => before.map((one) => (one.id === next.id ? next : one)))
          }
          onDelete={() => setUsers((before) => before.filter((one) => one.id !== user.id))}
        />
      ))}
      {cursor !== null && (
        <div className="line">
          <Button disabled={loading} onClick={() => void more()}>
            {t("item.loadMore")}
          </Button>
        </div>
      )}
    </>
  );
}
