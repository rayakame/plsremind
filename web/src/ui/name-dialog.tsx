import { useState, type FormEvent } from "react";
import { useT } from "../i18n/index.js";
import { Button, Field, Input } from "./controls.js";
import { Dialog } from "./dialog.js";

/** Asks for one name. */
export function NameDialog(props: {
  title: string;
  initial?: string;
  submit: string;
  onSubmit: (name: string) => void;
  onClose: () => void;
}) {
  const t = useT();
  const [name, setName] = useState(props.initial ?? "");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed === "") return;
    props.onSubmit(trimmed);
    props.onClose();
  };
  return (
    <Dialog title={props.title} onClose={props.onClose}>
      <form className="stack" onSubmit={submit}>
        <Field label={t("common.name")}>
          <Input value={name} onChange={(event) => setName(event.target.value)} required />
        </Field>
        <div className="line">
          <Button variant="primary" type="submit">
            {props.submit}
          </Button>
          <Button variant="ghost" onClick={props.onClose}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** Asks before something is deleted. */
export function ConfirmDialog(props: {
  title: string;
  text?: string;
  confirm: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const t = useT();
  return (
    <Dialog title={props.title} onClose={props.onClose}>
      {props.text && <p className="text">{props.text}</p>}
      <div className="line">
        <Button
          variant="danger"
          onClick={() => {
            props.onConfirm();
            props.onClose();
          }}
        >
          {props.confirm}
        </Button>
        <Button variant="ghost" onClick={props.onClose}>
          {t("common.cancel")}
        </Button>
      </div>
    </Dialog>
  );
}
