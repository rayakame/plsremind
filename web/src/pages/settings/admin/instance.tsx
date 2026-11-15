import type { Registration, Settings } from "plsremind";
import { useLoaded } from "../../../hub/loaded.js";
import { useHub } from "../../../hub/session.js";
import { useT } from "../../../i18n/index.js";
import { useNotices } from "../../../shell/notices.js";
import { Field, Input, Segmented } from "../../../ui/controls.js";
import { useDraft } from "../../../ui/draft.js";
import { Card } from "../../../ui/layout.js";

function Form(props: { settings: Settings; onChange: (next: Settings) => void }) {
  const t = useT();
  const { client } = useHub();
  const { guard } = useNotices();
  const save = async (changes: { name?: string; registration?: Registration }) => {
    const next = await guard(client.rest.updateSettings(changes));
    if (next) props.onChange(next);
  };
  const name = useDraft(props.settings.name, (next) => void save({ name: next }), {
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

export function AdminInstance() {
  const { client } = useHub();
  const settings = useLoaded((signal) => client.rest.getSettings({ signal }), client);
  if (!settings.data) return null;
  return <Form settings={settings.data} onChange={settings.setData} />;
}
