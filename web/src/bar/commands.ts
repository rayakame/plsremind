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

export interface Command {
  name: string;
  usage: string;
  describe: Key;
  needsItem?: true;
  /** What the command would do, in parts to show, or null if the text makes no sense. */
  preview: (rest: string, ctx: CommandContext) => readonly string[] | null;
  run: (rest: string, ctx: CommandContext) => Promise<unknown> | void;
}

const whenText = (when: When, now: Now) =>
  when.time
    ? formatMoment(instantAt(when.day, when.time, now.zone), now)
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
  const place = places[wanted];
  if (place !== undefined) return { path: place, label: wanted };
  const list = listOf(wanted, ctx.lists);
  return list ? { path: routes.list(list.id), label: list.name } : null;
}

const themes: readonly Prefs["theme"][] = ["system", "light", "dark"];

export const commands: readonly Command[] = [
  {
    name: "new",
    usage: "title [when] [!priority] [#list]",
    describe: "cmd.new",
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
    preview: (rest, ctx) => {
      try {
        const at = new Date(ctx.now.instant.getTime() + parseDuration(rest));
        return [t("cmd.remindAt", { time: formatMoment(at, ctx.now) })];
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
    preview: (rest) => {
      const theme = themes.find((one) => one === rest.toLowerCase());
      return theme ? [t(`theme.${theme}`)] : null;
    },
    run: (rest, ctx) => setPrefs(ctx.hub, { theme: rest.toLowerCase() as Prefs["theme"] }),
  },
];

export const commandNames = commands.map((command) => command.name);
