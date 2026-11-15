export const en = {
  "nav.all": "All items",
  "nav.open": "{count} open",
  "when.today": "today",
  "when.tomorrow": "tomorrow",
  "when.yesterday": "yesterday",
  "when.weekdays": "monday,tuesday,wednesday,thursday,friday,saturday,sunday",
  "when.weekdaysShort": "mon,tue,wed,thu,fri,sat,sun",
} as const;

export type Key = keyof typeof en;
export type Texts = Readonly<Record<Key, string>>;
