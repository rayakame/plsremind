import type { Connection } from "plsremind";
import { useState } from "react";
import { Link, useLocation } from "wouter";
import { useLoaded } from "../../../hub/loaded.js";
import { useNow } from "../../../hub/now.js";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { routes } from "../../../shell/router.js";
import { Avatar } from "../../../ui/avatar.js";
import { Button } from "../../../ui/controls.js";
import { Card } from "../../../ui/layout.js";
import { ConfirmDialog } from "../../../ui/name-dialog.js";
import { formatMoment } from "../../item/labels.js";
import { NotFound } from "../../not-found.js";
import { SecretDialog } from "../tokens.js";
import { AppForm, type AppDraft } from "./app-form.js";

const list = routes.settings("admin/apps");

function withoutToken({ token: _secret, ...app }: Connection): Connection {
  return app;
}

function AppList({ apps }: { apps: readonly Connection[] }) {
  const t = useT();
  const now = useNow();
  const { info } = useHub();
  const kindOf = (provider: string) =>
    info.loginMethods.find((method) => method.key === provider)?.name ?? provider;
  const sorted = [...apps].sort(
    (a, b) => kindOf(a.provider).localeCompare(kindOf(b.provider)) || a.name.localeCompare(b.name),
  );
  return (
    <>
      <div className="line">
        <Link href={`${list}/new/discord`} className="btn primary">
          {t("aapp.addDiscord")}
        </Link>
        <Link href={`${list}/new/other`} className="btn">
          {t("aapp.addOther")}
        </Link>
      </div>
      {apps.length === 0 && <p className="text dim">{t("aapp.none")}</p>}
      {apps.length > 0 && (
        <div role="list" className="rows">
          {sorted.map((app) => (
            <div role="listitem" className="row tall" key={app.id}>
              <Avatar name={app.name} square />
              <span className="fill stack0">
                <span className="line">
                  <span className="strong">{app.name}</span>
                  <span className="tag">{kindOf(app.provider)}</span>
                </span>
                <span className="dim small">
                  {t("aapp.reach", {
                    count: app.reach,
                    used: app.lastUsedAt
                      ? t("aapp.lastActive", { time: formatMoment(app.lastUsedAt, now) })
                      : t("aapp.neverUsed"),
                  })}
                  {!app.enabled && ` · ${t("aapp.off")}`}
                </span>
              </span>
              <Link href={`${list}/${app.id}`} className="btn ghost">
                {t("aapp.edit")}
              </Link>
            </div>
          ))}
        </div>
      )}
    </>
  );
}

function EditApp(props: { app: Connection; onChange: (next: Connection | undefined) => void }) {
  const t = useT();
  const { client, info } = useHub();
  const { guard } = useNotices();
  const [, navigate] = useLocation();
  const [busy, setBusy] = useState(false);
  const [dialog, setDialog] = useState<"rotate" | "delete">();
  const [token, setToken] = useState<string>();
  const { app } = props;

  const update = async (changes: Partial<AppDraft> & { enabled?: boolean }) => {
    setBusy(true);
    const next = await guard(client.rest.updateConnection(app.id, changes));
    setBusy(false);
    if (next) props.onChange(next);
  };
  const rotate = async () => {
    setDialog(undefined);
    const next = await guard(client.rest.rotateConnectionToken(app.id));
    if (next?.token) setToken(next.token);
  };
  const remove = async () => {
    setDialog(undefined);
    if ((await guard(client.rest.deleteConnection(app.id).then(() => true))) === true) {
      props.onChange(undefined);
      navigate(list);
    }
  };

  return (
    <>
      <Card>
        <AppForm
          kind={app.provider === "discord" ? "discord" : "other"}
          app={app}
          methods={info.loginMethods}
          submit={t("common.save")}
          busy={busy}
          onSubmit={({ provider: _kind, ...draft }) => void update(draft)}
        />
      </Card>
      <Card>
        <div className="line">
          <Button disabled={busy} onClick={() => setDialog("rotate")}>
            {t("aconn.newToken")}
          </Button>
          <Button disabled={busy} onClick={() => void update({ enabled: !app.enabled })}>
            {t(app.enabled ? "aapp.turnOff" : "aapp.turnOn")}
          </Button>
          <Button variant="danger" onClick={() => setDialog("delete")}>
            {t("common.delete")}
          </Button>
        </div>
      </Card>
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
          title={t("aconn.delete", { name: app.name })}
          text={t("aconn.deleteText")}
          confirm={t("common.delete")}
          onConfirm={() => void remove()}
          onClose={() => setDialog(undefined)}
        />
      )}
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

function NewApp(props: { kind: "discord" | "other"; onCreated: (app: Connection) => void }) {
  const t = useT();
  const { client, info } = useHub();
  const { guard } = useNotices();
  const [, navigate] = useLocation();
  const [busy, setBusy] = useState(false);
  const [token, setToken] = useState<string>();

  const create = async (draft: AppDraft) => {
    setBusy(true);
    const created = await guard(
      client.rest.createConnection({
        name: draft.name,
        provider: draft.provider,
        ...(draft.description ? { description: draft.description } : {}),
        allowedIntents: draft.allowedIntents,
        ...(draft.installUrl ? { installUrl: draft.installUrl } : {}),
      }),
    );
    setBusy(false);
    if (!created) return;
    const { token: secret, ...kept } = created;
    props.onCreated(kept);
    setToken(secret);
  };

  return (
    <>
      <Card>
        <AppForm
          kind={props.kind}
          methods={info.loginMethods}
          submit={t(props.kind === "discord" ? "aapp.createDiscord" : "aapp.createOther")}
          busy={busy}
          onSubmit={(draft) => void create(draft)}
        />
      </Card>
      {token !== undefined && (
        <SecretDialog
          title={t("aconn.token")}
          label={t("tokens.value")}
          value={token}
          hint={t("aapp.tokenHint")}
          onClose={() => {
            setToken(undefined);
            navigate(list);
          }}
        />
      )}
    </>
  );
}

export function AdminApps({ rest }: { rest: string | undefined }) {
  const { client } = useHub();
  const apps = useLoaded((signal) => client.rest.listConnections({ signal }), client);
  const add = (app: Connection) => apps.setData((before) => [...(before ?? []), app]);

  if (rest === "new/discord" || rest === "new/other") {
    return <NewApp kind={rest === "new/discord" ? "discord" : "other"} onCreated={add} />;
  }
  if (!apps.data) return null;
  if (rest === undefined) return <AppList apps={apps.data} />;
  const app = apps.data.find((one) => one.id === rest);
  if (!app) return <NotFound />;
  return (
    <EditApp
      key={app.id}
      app={app}
      onChange={(next) =>
        apps.setData((before) =>
          next
            ? before?.map((one) => (one.id === next.id ? withoutToken(next) : one))
            : before?.filter((one) => one.id !== app.id),
        )
      }
    />
  );
}
