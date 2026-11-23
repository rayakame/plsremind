import type { Connection, Intent, LoginMethod } from "plsremind";
import { useState, type FormEvent } from "react";
import { useT } from "../../../i18n/index.js";
import { Button, Field, Input, Toggle } from "../../../ui/controls.js";
import {
  discordApplicationIdOf,
  discordInstallUrl,
  isApplicationId,
  isInstallUrl,
} from "./apps-model.js";

const intents: readonly Intent[] = ["users", "lists", "items", "plans", "history", "deliveries"];

export interface AppDraft {
  name: string;
  description: string;
  provider: string;
  allowedIntents: Intent[];
  installUrl: string;
}

/** The fields of an app: a Discord bot asks for its Application ID, any other app for a link. */
export function AppForm(props: {
  kind: "discord" | "other";
  app?: Connection;
  methods: readonly LoginMethod[];
  submit: string;
  busy: boolean;
  onSubmit: (draft: AppDraft) => void;
}) {
  const t = useT();
  const { app } = props;
  const knownId = app ? discordApplicationIdOf(app.installUrl) : undefined;
  // A link someone set by hand is shown as it is rather than lost; the field stays while editing.
  const [byId] = useState(
    props.kind === "discord" && (!app || app.installUrl === "" || knownId !== undefined),
  );
  const [name, setName] = useState(app?.name ?? "");
  const [description, setDescription] = useState(app?.description ?? "");
  const [provider, setProvider] = useState(app?.provider ?? "");
  const [applicationId, setApplicationId] = useState(knownId ?? "");
  const [link, setLink] = useState(app?.installUrl ?? "");
  const [allowed, setAllowed] = useState<Intent[]>([...(app?.allowedIntents ?? intents)]);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const found: Record<string, string> = {};
    const id = applicationId.trim();
    if (byId && id !== "" && !isApplicationId(id)) found.applicationId = t("aapp.idError");
    if (!byId && !isInstallUrl(link.trim())) found.link = t("aapp.linkError");
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    props.onSubmit({
      name: name.trim(),
      description: description.trim(),
      provider: props.kind === "discord" ? "discord" : provider.trim(),
      allowedIntents: allowed,
      installUrl: byId ? (id === "" ? "" : discordInstallUrl(id)) : link.trim(),
    });
  };

  const kinds = props.methods.filter((method) => method.type !== "password");
  return (
    <form className="stack" onSubmit={submit}>
      <fieldset className="stack" disabled={props.busy}>
        <Field label={t("common.name")}>
          <Input value={name} onChange={(event) => setName(event.target.value)} required />
        </Field>
        <Field label={t("aapp.description")}>
          <Input value={description} onChange={(event) => setDescription(event.target.value)} />
        </Field>
        {props.kind === "other" && !app && (
          <Field label={t("aapp.kind")}>
            <Input
              value={provider}
              list="app-kinds"
              autoCapitalize="none"
              onChange={(event) => setProvider(event.target.value)}
              required
            />
          </Field>
        )}
        <datalist id="app-kinds">
          {kinds.map((method) => (
            <option key={method.key} value={method.key}>
              {method.name}
            </option>
          ))}
        </datalist>
        {byId ? (
          <Field label={t("aapp.applicationId")} error={errors.applicationId}>
            <Input
              className="mono"
              inputMode="numeric"
              value={applicationId}
              aria-invalid={errors.applicationId ? true : undefined}
              onChange={(event) => setApplicationId(event.target.value)}
            />
          </Field>
        ) : (
          <Field label={t("aapp.link")} error={errors.link}>
            <Input
              value={link}
              aria-invalid={errors.link ? true : undefined}
              onChange={(event) => setLink(event.target.value)}
            />
          </Field>
        )}
        {byId && <p className="text dim small">{t("aapp.idText")}</p>}
        <fieldset className="stack" aria-label={t("aapp.sees")}>
          <legend className="label">{t("aapp.sees")}</legend>
          {intents.map((intent) => (
            <Toggle
              key={intent}
              label={t(`intent.${intent}`)}
              checked={allowed.includes(intent)}
              onChange={(on) =>
                setAllowed(intents.filter((one) => (one === intent ? on : allowed.includes(one))))
              }
            />
          ))}
        </fieldset>
        <div className="line">
          <Button variant="primary" type="submit" disabled={props.busy}>
            {props.submit}
          </Button>
        </div>
      </fieldset>
    </form>
  );
}
