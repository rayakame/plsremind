import type { Item } from "plsremind";
import { useEffect, useMemo, useState } from "react";
import { useBar } from "../bar/context.js";
import { useHistory, useItems, useProfile } from "../hub/hooks.js";
import { useNow } from "../hub/now.js";
import { readPrefs } from "../hub/prefs.js";
import { locale, useT } from "../i18n/index.js";
import { byDay } from "../model/calendar.js";
import { useOpenItem, useSelection } from "../shell/selection.js";
import { timeIn } from "../time/zone.js";
import { addDays, monthGrid, weekStartOf } from "../time/zone.js";
import { Button, Segmented } from "../ui/controls.js";
import { Dialog } from "../ui/dialog.js";
import { Icon } from "../ui/icon.js";
import { useNarrow } from "../ui/layout-mode.js";
import { Row, Rows } from "../ui/layout.js";

type View = "month" | "week";

const shownPerDay = 3;
const noon = (day: string) => new Date(`${day}T12:00:00Z`);
const format = (day: string, options: Intl.DateTimeFormatOptions) =>
  new Intl.DateTimeFormat(locale(), { ...options, timeZone: "UTC" }).format(noon(day));

function storedView(): View {
  try {
    return sessionStorage.getItem("plsremind.calendar") === "week" ? "week" : "month";
  } catch {
    return "month";
  }
}

function shiftMonth(day: string, by: number): string {
  const [year, month] = day.split("-").map(Number);
  const moved = new Date(Date.UTC(year!, month! - 1 + by, 1));
  return moved.toISOString().slice(0, 10);
}

export default function CalendarPage() {
  const t = useT();
  const profile = useProfile();
  const { weekStart } = readPrefs(profile.preferences);
  const now = useNow();
  const open = useItems();
  const done = useHistory();
  const bar = useBar();
  const narrow = useNarrow();
  const { selected, select, setOrder } = useSelection();
  const openItem = useOpenItem();
  const [view, setView] = useState<View>(storedView);
  const [anchor, setAnchor] = useState(now.today);
  const [more, setMore] = useState<string>();

  const { setDefaultDue } = bar;
  useEffect(() => () => setDefaultDue(null), [setDefaultDue]);

  const days = useMemo(
    () =>
      view === "month"
        ? monthGrid(anchor.slice(0, 7), weekStart)
        : Array.from({ length: 7 }, (_, i) => addDays(weekStartOf(anchor, weekStart), i)),
    [view, anchor, weekStart],
  );
  const items = byDay(open, done, now.zone);
  const order = useMemo(
    () => days.flatMap((day) => (items.get(day) ?? []).map((item) => item.id)),
    [days, items],
  );
  useEffect(() => setOrder(order), [order, setOrder]);

  const changeView = (next: View) => {
    setView(next);
    try {
      sessionStorage.setItem("plsremind.calendar", next);
    } catch {
      // The view then lasts until the page changes.
    }
  };
  const step = (by: number) =>
    setAnchor(view === "month" ? shiftMonth(anchor, by) : addDays(anchor, by * 7));
  const pick = (item: Item) => (narrow ? openItem(item.id) : select(item.id));

  const event = (item: Item) => (
    <button
      key={item.id}
      type="button"
      className={[
        "ev",
        item.priority === "high" && "high",
        item.status === "done" && "done",
        item.id === selected && "sel",
      ]
        .filter(Boolean)
        .join(" ")}
      onClick={() => pick(item)}
    >
      {item.title}
    </button>
  );

  const chosen = bar.defaultDue;
  const month = anchor.slice(0, 7);

  return (
    <div className="scroll col">
      <div className="between">
        <h1 className="h1">{format(anchor, { month: "long", year: "numeric" })}</h1>
        <div className="line">
          <Button variant="icon" aria-label={t("item.previous")} onClick={() => step(-1)}>
            <Icon name="left" />
          </Button>
          <Button onClick={() => setAnchor(now.today)}>{t("cal.today")}</Button>
          <Button variant="icon" aria-label={t("item.next")} onClick={() => step(1)}>
            <Icon name="right" />
          </Button>
          <Segmented
            label={t("cal.view")}
            value={view}
            options={[
              { value: "month", label: t("cal.month") },
              { value: "week", label: t("cal.week") },
            ]}
            onChange={changeView}
          />
        </div>
      </div>
      <div className={view === "week" ? "cal week" : "cal"} role="group" aria-label={t("cal.days")}>
        {days.slice(0, 7).map((day) => (
          <div className="hd" key={day}>
            {format(day, { weekday: "short" })}
          </div>
        ))}
        {days.map((day) => {
          const own = items.get(day) ?? [];
          const shown = view === "month" ? own.slice(0, shownPerDay) : own;
          const names = [
            "day",
            view === "month" && day.slice(0, 7) !== month && "out",
            day === now.today && "today",
            day === chosen && "sel",
          ];
          return (
            <div className={names.filter(Boolean).join(" ")} data-day={day} key={day}>
              <button
                type="button"
                className="n"
                aria-label={format(day, { day: "numeric", month: "long" })}
                aria-pressed={day === chosen}
                aria-current={day === now.today ? "date" : undefined}
                onClick={() => setDefaultDue(day === chosen ? null : day)}
              >
                {Number(day.slice(8))}
                {narrow && own.length > 0 && <span className="count"> {own.length}</span>}
              </button>
              {shown.map(event)}
              {own.length > shown.length && (
                <button type="button" className="ev" onClick={() => setMore(day)}>
                  {t("cal.more", { count: own.length - shown.length })}
                </button>
              )}
            </div>
          );
        })}
      </div>
      {narrow && chosen && days.includes(chosen) && (
        <DayList day={chosen} items={items.get(chosen) ?? []} zone={now.zone} onPick={pick} />
      )}
      {more && (
        <Dialog
          title={format(more, { weekday: "long", day: "numeric", month: "long" })}
          onClose={() => setMore(undefined)}
        >
          <DayList
            day={more}
            items={items.get(more) ?? []}
            zone={now.zone}
            onPick={(item) => {
              setMore(undefined);
              pick(item);
            }}
          />
        </Dialog>
      )}
    </div>
  );
}

function DayList(props: {
  day: string;
  items: readonly Item[];
  zone: string;
  onPick: (item: Item) => void;
}) {
  const t = useT();
  if (props.items.length === 0) return <p className="text dim">{t("cal.nothing")}</p>;
  return (
    <Rows role="list">
      {props.items.map((item) => (
        <Row
          key={item.id}
          role="listitem"
          className="click"
          done={item.status === "done"}
          onClick={() => props.onPick(item)}
        >
          <button type="button" className="t ev-plain">
            {item.title}
          </button>
          <span className="meta">
            {item.allDay || !item.dueAt ? "" : timeIn(item.dueAt, props.zone)}
          </span>
        </Row>
      ))}
    </Rows>
  );
}
