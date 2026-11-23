import { useProfile } from "../../hub/hooks.js";
import { isPrefix, readPrefs } from "../../hub/prefs.js";
import { useHub } from "../../hub/session.js";
import { setPrefs } from "../../hub/write.js";
import { useT } from "../../i18n/index.js";
import { keysOf } from "../../keys/actions.js";
import { useNotices } from "../../shell/notices.js";
import { Field, Input } from "../../ui/controls.js";
import { useCheckedDraft } from "../../ui/draft.js";
import { Card } from "../../ui/layout.js";
import { KeysEditor } from "./keys-editor.js";

export function KeySettings() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const prefs = readPrefs(useProfile().preferences);
  const taken = new Set(Object.values(keysOf(prefs.keys)));
  const prefix = useCheckedDraft(
    prefs.prefix,
    (next) => (isPrefix(next) && !taken.has(next) ? next : undefined),
    (next) => void guard(setPrefs(hub, { prefix: next })),
    { enter: true },
  );

  return (
    <Card>
      <Field
        label={t("personal.prefix")}
        error={prefix.invalid ? t("personal.prefixError") : undefined}
      >
        <Input maxLength={2} {...prefix.field} />
      </Field>
      <KeysEditor />
    </Card>
  );
}
