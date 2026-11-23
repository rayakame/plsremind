import type { Invite } from "plsremind";
import { useState } from "react";
import { useLoaded } from "../../../hub/loaded.js";
import { useNow } from "../../../hub/now.js";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Button, Field, Select } from "../../../ui/controls.js";
import { Card } from "../../../ui/layout.js";
import { formatMoment } from "../../item/labels.js";
import { SecretDialog } from "../tokens.js";

const day = 86_400_000;
const lifetimes = [1, 7, 30];

export function AdminInvites() {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const now = useNow();
  const invites = useLoaded((signal) => client.rest.listInvites({ signal }), client);
  const [days, setDays] = useState(7);
  const [code, setCode] = useState<string>();
  const [busy, setBusy] = useState(false);

  const create = async () => {
    if (busy) return;
    setBusy(true);
    const created = await guard(client.rest.createInvite({ expiresIn: days * day }));
    setBusy(false);
    if (!created) return;
    const { code: secret, ...kept } = created;
    setCode(secret);
    invites.setData((before) => [...(before ?? []), kept]);
  };
  const remove = async (invite: Invite) => {
    if ((await guard(client.rest.deleteInvite(invite.id).then(() => true))) === true) {
      invites.setData((before) => before?.filter((one) => one.id !== invite.id));
    }
  };
  const state = (invite: Invite) =>
    invite.usedAt
      ? t("invite.used")
      : invite.expiresAt.getTime() < now.instant.getTime()
        ? t("invite.expired")
        : t("invite.open");

  return (
    <>
      <Card>
        {invites.data?.length === 0 && <p className="text dim">{t("invite.none")}</p>}
        <div role="list" className="stack" aria-label={t("admin.invites")}>
          {invites.data?.map((invite) => (
            <div role="listitem" className="between" key={invite.id}>
              <span className="kv fill">
                <span>{state(invite)}</span>
                <span>{t("invite.until", { time: formatMoment(invite.expiresAt, now) })}</span>
              </span>
              <Button onClick={() => void remove(invite)}>{t("common.delete")}</Button>
            </div>
          ))}
        </div>
      </Card>
      <Card title={t("invite.create")}>
        <Field label={t("invite.validFor")}>
          <Select value={days} onChange={(event) => setDays(Number(event.target.value))}>
            {lifetimes.map((count) => (
              <option key={count} value={count}>
                {count === 1 ? t("invite.day") : t("invite.days", { count })}
              </option>
            ))}
          </Select>
        </Field>
        <div className="line">
          <Button variant="primary" disabled={busy} onClick={() => void create()}>
            {t("invite.create")}
          </Button>
        </div>
      </Card>
      {code !== undefined && (
        <SecretDialog
          title={t("invite.new")}
          label={t("invite.code")}
          value={code}
          onClose={() => setCode(undefined)}
        />
      )}
    </>
  );
}
