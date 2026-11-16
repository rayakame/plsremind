import { afterEach } from "vitest";

if (typeof HTMLElement !== "undefined") {
  // happy-dom lays nothing out, and a virtual list draws only what fits its measured box.
  for (const [name, size] of [
    ["clientHeight", 600],
    ["clientWidth", 800],
  ] as const) {
    Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get: () => size });
  }

  // The style file is not loaded in tests, and a virtual list needs the height of a row.
  document.documentElement.style.setProperty("--h", "44px");

  afterEach(() => {
    sessionStorage.clear();
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
}
