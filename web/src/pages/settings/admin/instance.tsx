import type { Client, Registration, Settings } from "plsremind";
import { useLoaded } from "../../../hub/loaded.js";
import { useHub, useSessionStore } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Field, Input, Segmented } from "../../../ui/controls.js";
import { useDraft } from "../../../ui/draft.js";
import { Card } from "../../../ui/layout.js";

const saving = new WeakMap<Client, Promise<unknown>>();

function Form(props: { settings: Settings; onChange: (next: Settings) => void }) {
  const t = useT();
  const { client } = useHub();
  const session = useSessionStore();
  const { guard } = useNotices();
  // One after the other: each answer carries all settings and would undo a newer change.
  const save = (changes: { name?: string; registration?: Registration }) => {
    const saved = (saving.get(client) ?? Promise.resolve()).then(async () => {
      const next = await guard(client.rest.updateSettings(changes));
      if (!next) return false;
      props.onChange(next);
      session.rename(next.name);
      return true;
    });
    saving.set(client, saved);
    return saved;
  };
  const name = useDraft(props.settings.name, (next) => save({ name: next }), {
    required: true,
    enter: true,
  });
  return (
    <Card>
      <Field label={t("admin.name")}>
        <Input {...name} />
      </Field>
      <div className="between">
        <span>{t("admin.registration")}</span>
        <Segmented<Registration>
          label={t("admin.registration")}
          value={props.settings.registration}
          options={[
            { value: "open", label: t("admin.registrationOpen") },
            { value: "invite", label: t("admin.registrationInvite") },
          ]}
          onChange={(registration) => void save({ registration })}
        />
      </div>
    </Card>
  );
}

const releases = "https://github.com/rayakame/plsremind/releases/tag/v";

function Version() {
  const t = useT();
  const { info } = useHub();
  const released = /^\d+\.\d+\.\d+$/.test(info.version);
  return (
    <Card title={t("admin.version")}>
      <div className="between">
        <span>{t("admin.versionRunning", { version: info.version })}</span>
        {released && (
          <a href={releases + info.version} target="_blank" rel="noreferrer">
            {t("admin.versionNotes")}
          </a>
        )}
      </div>
    </Card>
  );
}

export function AdminInstance() {
  const { client } = useHub();
  const settings = useLoaded(async (signal) => {
    // A save from before this form was shown has to land first, or the form shows the old state.
    await saving.get(client)?.catch(() => {});
    return client.rest.getSettings({ signal });
  }, client);
  return (
    <>
      {settings.data && <Form settings={settings.data} onChange={settings.setData} />}
      <Version />
    </>
  );
}
