import { afterEach, expect, test } from "vitest";
import { de } from "../src/i18n/de.js";
import { en } from "../src/i18n/en.js";
import { locale, setLocale, t } from "../src/i18n/index.js";

afterEach(() => setLocale("en"));

test("every language has exactly the keys of English", () => {
  expect(Object.keys(de).sort()).toEqual(Object.keys(en).sort());
});

test("no text is empty or carries an emoji", () => {
  for (const [name, texts] of Object.entries({ en, de })) {
    for (const [key, text] of Object.entries(texts)) {
      expect(text, `${name} ${key}`).not.toBe("");
      expect(/\p{Extended_Pictographic}/u.test(text), `${name} ${key}`).toBe(false);
    }
  }
});

test("a placeholder appears in both languages wherever it appears in one", () => {
  const holes = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
  for (const key of Object.keys(en) as (keyof typeof en)[]) {
    expect(holes(de[key]), key).toEqual(holes(en[key]));
  }
});

test("texts follow the chosen language and fill their placeholders", () => {
  expect(t("nav.open", { count: 3 })).toBe("3 open");
  setLocale("de-AT");
  expect(locale()).toBe("de-AT");
  expect(t("nav.open", { count: 3 })).toBe("3 offen");
  setLocale("fr");
  expect(t("nav.all")).toBe("All items");
  expect(locale()).toBe("en");
  setLocale(null);
  expect(t("nav.all")).toBe("All items");
});
