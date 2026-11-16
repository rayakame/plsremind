import { parseDuration, type Item, type List, type Priority } from "plsremind";
import type { Prefs } from "../hub/prefs.js";
import {
  addReminder,
  createItem,
  patchItem,
  setDone,
  setPrefs,
  type Writer,
} from "../hub/write.js";
import { t, type Key } from "../i18n/index.js";
import type { Now } from "../model/groups.js";
import { routes } from "../shell/router.js";
import { formatDay, formatMoment } from "../time/format.js";
import { parseWhen, type When } from "../time/when.js";
import { instantAt } from "../time/zone.js";
import { listOf, parseNew, priorityOf } from "./parse.js";

export interface CommandContext {
  hub: Writer;
  lists: readonly List[];
  /** The item the command is about, if one is selected or open. */
  item: Item | undefined;
  /** The list that is open, for new items. */
  listId: string | undefined;
  /** The day chosen in the calendar, for new items without a time of their own. */
  defaultDue: string | null;
  now: Now;
  navigate: (path: string) => void;
}

/** One value a command's argument may take, offered while it is typed. */
export interface Choice {
  /** What the bar holds once this is chosen. */
  text: string;
  label: string;
  describe?: string;
  /** True if choosing it completes the command, so that it can run at once. */
  run: boolean;
}

export interface Command {
  name: string;
  usage: string;
  describe: Key;
  needsItem?: true;
  /** Examples of what may be typed, shown while the argument is still empty. */
  example?: Key;
  /** The values that fit what is typed so far. */
  choices?: (rest: string, ctx: CommandContext, prefix: string) => readonly Choice[];
  /** What the command would do, in parts to show, or null if the text makes no sense. */
  preview: (rest: string, ctx: CommandContext) => readonly string[] | null;
  run: (rest: string, ctx: CommandContext) => Promise<unknown> | void;
}

const whenText = (when: When, now: Now) =>
  when.time
    ? formatMoment(instantAt(when.day, when.time, now.zone), now.zone, now.today)
    : formatDay(when.day, now.today);

const dayOnly = (day: string | null): When | null => (day ? { day, time: null } : null);

const places: Record<string, string> = {
  all: routes.items,
  calendar: routes.calendar,
  done: routes.done,
  settings: routes.settings(),
};

function placeOf(rest: string, ctx: CommandContext): { path: string; label: string } | null {
  const wanted = rest.trim().toLowerCase();
  if (wanted === "") return null;
  // With the hash a list is meant, also one that is named like a page.
  const place = Object.hasOwn(places, wanted) ? places[wanted] : undefined;
  if (place !== undefined) return { path: place, label: wanted };
  const list = listOf(wanted, ctx.lists);
  return list ? { path: routes.list(list.id), label: list.name } : null;
}

const themes: readonly Prefs["theme"][] = ["system", "light", "dark"];
const priorities: readonly Priority[] = ["low", "normal", "high"];
const starts = (text: string, typed: string) => text.toLowerCase().startsWith(typed.toLowerCase());

/** Choices that fill the whole argument of a command. */
function whole(
  name: string,
  rest: string,
  prefix: string,
  values: readonly { value: string; label?: string; describe?: string }[],
): Choice[] {
  return values
    .filter((one) => !rest.startsWith("#") || one.label?.startsWith("#"))
    .filter((one) => starts(one.value, rest.replace(/^#/, "")) || starts(one.describe ?? "", rest))
    .map((one) => ({
      text: `${prefix}${name} ${one.label ?? one.value}`,
      label: one.label ?? one.value,
      ...(one.describe === undefined ? {} : { describe: one.describe }),
      run: true,
    }));
}

/** Choices for the word being typed inside a new item: lists after "#", priorities after "!". */
function insideNew(rest: string, ctx: CommandContext, prefix: string): Choice[] {
  const word = /(^|\s)([#!]\S*)$/.exec(rest)?.[2];
  if (word === undefined) return [];
  const before = rest.slice(0, rest.length - word.length);
  const typed = word.slice(1);
  const fill = (marker: string) => `${prefix}new ${before}${marker} `;
  if (word.startsWith("!")) {
    return priorities
      .filter((one) => starts(one, typed) || starts(t(`priority.${one}`), typed))
      .map((one) => ({
        text: fill(`!${one}`),
        label: `!${one}`,
        describe: t(`priority.${one}`),
        run: false,
      }));
  }
  return ctx.lists.flatMap((list) => {
    // A new item's text is read word by word, so a list is named by its first word.
    const short = list.name.split(/\s+/)[0]!.toLowerCase();
    if (!starts(list.name, typed) || listOf(short, ctx.lists) !== list) return [];
    return [{ text: fill(`#${short}`), label: `#${list.name}`, run: false }];
  });
}

export const commands: readonly Command[] = [
  {
    name: "new",
    usage: "title [when] [!priority] [#list]",
    describe: "cmd.new",
    choices: insideNew,
    preview: (rest, ctx) => {
      const draft = parseNew(rest, { today: ctx.now.today, lists: ctx.lists });
      if (draft.title === "") return null;
      const when = draft.when ?? dayOnly(ctx.defaultDue);
      return [
        draft.title,
        ...(when ? [whenText(when, ctx.now)] : []),
        ...(draft.priority ? [t(`priority.${draft.priority}`)] : []),
        ...(draft.list ? [draft.list.name] : []),
        ...(draft.unknownList !== null ? [t("cmd.noList", { name: draft.unknownList })] : []),
      ];
    },
    run: (rest, ctx) => {
      const draft = parseNew(rest, { today: ctx.now.today, lists: ctx.lists });
      return createItem(ctx.hub, {
        title: draft.title,
        listId: draft.list?.id ?? ctx.listId,
        priority: draft.priority ?? undefined,
        due: draft.when ?? dayOnly(ctx.defaultDue) ?? undefined,
      });
    },
  },
  {
    name: "find",
    usage: "text",
    describe: "cmd.find",
    preview: (rest) => (rest === "" ? null : [rest]),
    run: (rest, ctx) => ctx.navigate(`/find?q=${encodeURIComponent(rest)}`),
  },
  {
    name: "go",
    usage: "place",
    describe: "cmd.go",
    choices: (rest, ctx, prefix) =>
      whole("go", rest, prefix, [
        { value: "all", describe: t("nav.all") },
        { value: "calendar", describe: t("nav.calendar") },
        { value: "done", describe: t("nav.done") },
        { value: "settings", describe: t("nav.settings") },
        ...ctx.lists.map((list) => ({
          value: list.name,
          label: `#${list.name}`,
          describe: t("item.list"),
        })),
      ]),
    preview: (rest, ctx) => {
      const place = placeOf(rest, ctx);
      return place ? [place.label] : null;
    },
    run: (rest, ctx) => ctx.navigate(placeOf(rest, ctx)!.path),
  },
  {
    name: "done",
    usage: "",
    describe: "cmd.done",
    needsItem: true,
    preview: (rest, ctx) => (rest === "" ? [ctx.item!.title] : null),
    run: (_rest, ctx) => setDone(ctx.hub, ctx.item!, true),
  },
  {
    name: "remind",
    usage: "duration",
    describe: "cmd.remind",
    needsItem: true,
    example: "cmd.remindExample",
    preview: (rest, ctx) => {
      try {
        const at = new Date(ctx.now.instant.getTime() + parseDuration(rest));
        return [t("cmd.remindAt", { time: formatMoment(at, ctx.now.zone, ctx.now.today) })];
      } catch {
        return null;
      }
    },
    run: (rest, ctx) => addReminder(ctx.hub, ctx.item!, new Date(Date.now() + parseDuration(rest))),
  },
  {
    name: "due",
    usage: "when",
    describe: "cmd.due",
    needsItem: true,
    example: "cmd.dueExample",
    preview: (rest, ctx) => {
      if (rest === "-") return [t("cmd.noDue")];
      const when = parseWhen(rest, ctx.now.today);
      return when ? [whenText(when, ctx.now)] : null;
    },
    run: (rest, ctx) =>
      patchItem(ctx.hub, ctx.item!, { due: rest === "-" ? null : parseWhen(rest, ctx.now.today) }),
  },
  {
    name: "move",
    usage: "#list",
    describe: "cmd.move",
    needsItem: true,
    choices: (rest, ctx, prefix) =>
      whole(
        "move",
        rest,
        prefix,
        ctx.lists.map((list) => ({ value: list.name, label: `#${list.name}` })),
      ),
    preview: (rest, ctx) => {
      const list = listOf(rest, ctx.lists);
      return list ? [list.name] : null;
    },
    run: (rest, ctx) => patchItem(ctx.hub, ctx.item!, { listId: listOf(rest, ctx.lists)!.id }),
  },
  {
    name: "prio",
    usage: "low|normal|high",
    describe: "cmd.prio",
    needsItem: true,
    choices: (rest, _ctx, prefix) =>
      whole(
        "prio",
        rest,
        prefix,
        priorities.map((one) => ({ value: one, describe: t(`priority.${one}`) })),
      ),
    preview: (rest) => {
      const priority = priorityOf(rest);
      return priority ? [t(`priority.${priority}`)] : null;
    },
    run: (rest, ctx) => patchItem(ctx.hub, ctx.item!, { priority: priorityOf(rest) as Priority }),
  },
  {
    name: "theme",
    usage: "system|light|dark",
    describe: "cmd.theme",
    choices: (rest, _ctx, prefix) =>
      whole(
        "theme",
        rest,
        prefix,
        themes.map((one) => ({ value: one, describe: t(`theme.${one}`) })),
      ),
    preview: (rest) => {
      const theme = themes.find((one) => one === rest.toLowerCase());
      return theme ? [t(`theme.${theme}`)] : null;
    },
    run: (rest, ctx) => setPrefs(ctx.hub, { theme: rest.toLowerCase() as Prefs["theme"] }),
  },
];

export const commandNames = commands.map((command) => command.name);
