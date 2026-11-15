import { useEffect, useId, useState, type KeyboardEvent } from "react";
import { useLocation, useRoute } from "wouter";
import { useItem, useLists } from "../hub/hooks.js";
import { useNow } from "../hub/now.js";
import { useHub } from "../hub/session.js";
import { createItem, type ItemDraft } from "../hub/write.js";
import { useT } from "../i18n/index.js";
import { useNotices } from "../shell/notices.js";
import { useSelection } from "../shell/selection.js";
import { Button } from "../ui/controls.js";
import { Icon } from "../ui/icon.js";
import { Kbd } from "../ui/layout.js";
import { commandNames, commands, type Command, type CommandContext } from "./commands.js";
import { useBar } from "./context.js";
import { complete, parseCommand } from "./parse.js";
import { QuickForm } from "./quick-form.js";

/** The field on every page that filters, creates and takes commands. */
export function Bar() {
  const t = useT();
  const hub = useHub();
  const { guard } = useNotices();
  const bar = useBar();
  const lists = useLists();
  const now = useNow();
  const { selected } = useSelection();
  const [, navigate] = useLocation();
  const [, onList] = useRoute("/lists/:id");
  const [, onItem] = useRoute("/items/:id");
  const item = useItem(onItem?.id ?? selected);
  const [form, setForm] = useState(false);
  const [at, setAt] = useState(0);
  const listbox = useId();
  const listId = onList?.id;

  const typed = parseCommand(bar.text, bar.prefix);
  const naming = typed !== null && !/\s/.test(bar.text);
  const exact = typed ? commands.find((command) => command.name === typed.name) : undefined;
  const offered: readonly Command[] = !typed
    ? []
    : naming
      ? commands.filter((command) => command.name.startsWith(typed.name))
      : exact
        ? [exact]
        : [];
  useEffect(() => setAt(0), [bar.text]);

  const ctx: CommandContext = {
    hub,
    lists,
    item,
    listId,
    defaultDue: bar.defaultDue,
    now,
    navigate,
  };
  const understood =
    !typed || !exact
      ? null
      : exact.needsItem && !item
        ? "needsItem"
        : exact.preview(typed.rest, ctx);

  const create = (draft: ItemDraft) => void guard(createItem(hub, draft));
  const submit = () => {
    if (typed) return run();
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
  function run() {
    if (!typed) return;
    if (exact && Array.isArray(understood)) {
      const work = exact.run(typed.rest, ctx);
      bar.clear();
      if (work) void guard(work);
    } else if (!exact && offered[at]) {
      bar.setText(`${bar.prefix}${offered[at].name} `);
    }
  }

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      bar.clear();
    } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    } else if (event.key === "Enter" && typed) {
      event.preventDefault();
      run();
    } else if (event.key === "Tab" && typed) {
      const finished = naming
        ? offered[at]
          ? `${bar.prefix}${offered[at].name} `
          : null
        : complete(bar.text, bar.prefix, commandNames);
      if (finished !== null) {
        event.preventDefault();
        bar.setText(finished);
      }
    } else if (typed && naming && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setAt((offered.length + at + step) % Math.max(1, offered.length));
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
            aria-expanded={typed !== null}
            aria-controls={typed ? listbox : undefined}
            aria-activedescendant={
              naming && offered[at] ? `${listbox}-${offered[at].name}` : undefined
            }
            aria-label={t("bar.label")}
            placeholder={t("bar.placeholder", { prefix: bar.prefix })}
            className={typed ? "mono" : undefined}
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
        {typed && (
          <div className="pal">
            <div className="hint" role="status" aria-label={t("cmd.understood")}>
              {!exact && offered.length === 0 && <span>{t("cmd.unknown")}</span>}
              {exact && understood === "needsItem" && <span>{t("cmd.needsItem")}</span>}
              {exact && understood === null && (
                <span className="mono">{`${bar.prefix}${exact.name} ${exact.usage}`.trim()}</span>
              )}
              {Array.isArray(understood) &&
                understood.map((part, index) => <span key={index}>{part}</span>)}
            </div>
            <div id={listbox} role="listbox" aria-label={t("cmd.commands")}>
              {offered.map((command, index) => (
                <button
                  key={command.name}
                  type="button"
                  role="option"
                  id={`${listbox}-${command.name}`}
                  tabIndex={-1}
                  aria-selected={naming && index === at}
                  className={naming && index === at ? "opt on" : "opt"}
                  onClick={() => bar.focus(`${bar.prefix}${command.name} `)}
                >
                  <span className="mono">
                    {`${bar.prefix}${command.name} ${command.usage}`.trim()}
                  </span>
                  <span className="t dim">{t(command.describe)}</span>
                </button>
              ))}
            </div>
          </div>
        )}
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
