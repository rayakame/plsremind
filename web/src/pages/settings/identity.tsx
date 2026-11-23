import type { Identity } from "plsremind";
import { useHub } from "../../hub/session.js";
import { useT } from "../../i18n/index.js";
import { Avatar } from "../../ui/avatar.js";
import { Button } from "../../ui/controls.js";
import { Icon } from "../../ui/icon.js";
import { ConfirmDialog } from "../../ui/name-dialog.js";

/** One linked account: its picture, name, handle and provider, and a way to unlink it. */
export function IdentityRow(props: { identity: Identity; provider: string; onUnlink: () => void }) {
  const t = useT();
  const { client } = useHub();
  const { identity } = props;
  const picture = identity.hasAvatar ? client.rest.identityAvatarUrl(identity.id) : undefined;
  return (
    <div role="listitem" className="who">
      <Avatar name={identity.displayName} src={picture} />
      <span className="fill stack0">
        <span className="strong">{identity.displayName}</span>
        <span className="dim">
          {identity.username ? `@${identity.username} · ${props.provider}` : props.provider}
        </span>
      </span>
      <span className="ok">
        <Icon name="check" />
        {t("accounts.linked")}
      </span>
      <Button variant="ghost" onClick={props.onUnlink}>
        {t("accounts.unlink")}
      </Button>
    </div>
  );
}

/** Asks before an account is unlinked, the same way wherever that happens. */
export function UnlinkDialog(props: {
  provider: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const t = useT();
  return (
    <ConfirmDialog
      title={t("accounts.unlinkTitle", { name: props.provider })}
      text={t("accounts.unlinkText")}
      confirm={t("accounts.unlink")}
      onConfirm={props.onConfirm}
      onClose={props.onClose}
    />
  );
}
