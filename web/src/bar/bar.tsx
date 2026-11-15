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
import {
  commandNames,
  commands,
  type Choice,
  type Command,
  type CommandContext,
} from "./commands.js";
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
  useEffect(() => setAt(naming ? 0 : -1), [bar.text, naming]);

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
  const choices: readonly Choice[] =
    typed && exact && !naming ? (exact.choices?.(typed.rest, ctx, bar.prefix) ?? []) : [];

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
  /** Runs a command as written. One that cannot run yet is left in the bar. */
  function execute(text: string) {
    const written = parseCommand(text, bar.prefix);
    const command = written && commands.find((one) => one.name === written.name);
    const ready =
      written && command && !(command.needsItem && !item) && command.preview(written.rest, ctx);
    if (!written || !command || !ready) return bar.setText(text);
    const work = command.run(written.rest, ctx);
    bar.clear();
    if (work) void guard(work);
  }
  function run() {
    if (!typed) return;
    if (exact) execute(bar.text);
    else if (offered[at]) bar.setText(`${bar.prefix}${offered[at].name} `);
  }
  const pick = (choice: Choice) => (choice.run ? execute(choice.text) : bar.focus(choice.text));

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      bar.clear();
    } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    } else if (event.key === "Enter" && typed) {
      event.preventDefault();
      const chosen = naming ? undefined : choices[at];
      if (chosen) pick(chosen);
      else run();
    } else if (event.key === "Tab" && typed) {
      const finished = naming
        ? offered[at]
          ? `${bar.prefix}${offered[at].name} `
          : null
        : (choices[Math.max(at, 0)]?.text ?? complete(bar.text, bar.prefix, commandNames));
      if (finished !== null) {
        event.preventDefault();
        bar.setText(finished);
      }
    } else if (typed && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
      const count = naming ? offered.length : choices.length;
      if (count === 0) return;
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setAt(at < 0 && step < 0 ? count - 1 : (count + at + step) % count);
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
              naming && offered[at]
                ? `${listbox}-${offered[at].name}`
                : choices[at]
                  ? `${listbox}-${at}`
                  : undefined
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
              {exact && understood === null && typed.rest === "" && exact.example && (
                <span>{t(exact.example)}</span>
              )}
              {exact && understood === null && !(typed.rest === "" && exact.example) && (
                <span className="mono">{`${bar.prefix}${exact.name} ${exact.usage}`.trim()}</span>
              )}
              {Array.isArray(understood) &&
                understood.map((part, index) => <span key={index}>{part}</span>)}
            </div>
            <div id={listbox} role="listbox" aria-label={t("cmd.commands")}>
              {choices.map((choice, index) => (
                <button
                  key={choice.text}
                  type="button"
                  role="option"
                  id={`${listbox}-${index}`}
                  tabIndex={-1}
                  aria-selected={index === at}
                  className={index === at ? "opt on" : "opt"}
                  onClick={() => pick(choice)}
                >
                  <span className="mono">{choice.label}</span>
                  {choice.describe !== undefined && (
                    <span className="t dim">{choice.describe}</span>
                  )}
                </button>
              ))}
              {(choices.length > 0 ? [] : offered).map((command, index) => (
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
