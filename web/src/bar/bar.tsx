import { useState, type KeyboardEvent } from "react";
import { useRoute } from "wouter";
import { useHub } from "../hub/session.js";
import { createItem, type ItemDraft } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { useNotices } from "../shell/notices.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { Kbd } from "../ui/layout.js";
import { useBar } from "./context.js";
import { QuickForm } from "./quick-form.js";

/** The field on every page that filters, creates and takes commands. */
export function Bar() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const bar = useBar();
  const [, params] = useRoute("/lists/:id");
  const [form, setForm] = useState(false);
  const listId = params?.id;

  const create = (draft: ItemDraft) => void guard(createItem(hub, draft));
  const submit = () => {
    const title = bar.filter.trim();
    if (bar.text.trim() === "") return setForm(true);
    if (title === "") return;
    create({
      title,
      listId,
      due: bar.defaultDue ? { day: bar.defaultDue, time: null } : undefined,
    });
    bar.setText("");
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      bar.clear();
    } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    }
  };

  return (
    <>
      <div className="barwrap">
        <div className="bar">
          <Icon name="search" />
          <input
            ref={bar.input}
            role="combobox"
            aria-expanded={false}
            aria-label={t("bar.label")}
            placeholder={t("bar.placeholder", { prefix: bar.prefix })}
            value={bar.text}
            onChange={(event) => bar.setText(event.target.value)}
            onKeyDown={onKeyDown}
            autoComplete="off"
            spellCheck={false}
          />
          <span className="only-wide">
            <Kbd>{bar.prefix}</Kbd>
          </span>
        </div>
      </div>
      <Button variant="primary" aria-label={t("common.create")} onClick={submit}>
        <Icon name="plus" />
        <span className="only-wide">{t("common.create")}</span>
      </Button>
      {form && (
        <QuickForm
          listId={listId}
          day={bar.defaultDue}
          onCreate={create}
          onClose={() => setForm(false)}
        />
      )}
    </>
  );
}
