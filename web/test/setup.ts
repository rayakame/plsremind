if (typeof HTMLElement !== "undefined") {
  // happy-dom lays nothing out, and a virtual list draws only what fits its measured box.
  for (const [name, size] of [
    ["clientHeight", 600],
    ["clientWidth", 800],
  ] as const) {
    Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get: () => size });
  }
}
