import type { Page } from "@playwright/test";

/** Text whose contrast against what lies behind it is below 4.5, and controls without a name. */
export async function audit(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const parse = (color: string) => {
      const parts = color.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0, 0];
      return { r: parts[0]!, g: parts[1]!, b: parts[2]!, a: parts[3] ?? 1 };
    };
    const luminance = ({ r, g, b }: { r: number; g: number; b: number }) => {
      const channel = (value: number) => {
        const s = value / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };
    const behind = (element: Element) => {
      let at: Element | null = element;
      const layers: { r: number; g: number; b: number; a: number }[] = [];
      while (at) {
        const color = parse(getComputedStyle(at).backgroundColor);
        if (color.a > 0) layers.push(color);
        if (color.a === 1) break;
        at = at.parentElement;
      }
      let result = { r: 255, g: 255, b: 255 };
      for (const layer of layers.reverse()) {
        result = {
          r: layer.r * layer.a + result.r * (1 - layer.a),
          g: layer.g * layer.a + result.g * (1 - layer.a),
          b: layer.b * layer.a + result.b * (1 - layer.a),
        };
      }
      return result;
    };
    const visible = (element: Element) => {
      const box = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      return box.width > 0 && box.height > 0 && style.visibility !== "hidden";
    };

    const spoken = (control: Element) => {
      const copy = control.cloneNode(true) as Element;
      for (const hidden of copy.querySelectorAll('[aria-hidden="true"], [hidden]')) hidden.remove();
      return copy.textContent;
    };

    const problems: string[] = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const seen = new Set<Element>();
    while (walker.nextNode()) {
      const element = walker.currentNode.parentElement;
      if (!element || seen.has(element) || !walker.currentNode.textContent?.trim()) continue;
      seen.add(element);
      if (!visible(element) || element.closest("[disabled], .sr")) continue;
      const back = behind(element);
      const ink = parse(getComputedStyle(element).color);
      const text = {
        r: ink.r * ink.a + back.r * (1 - ink.a),
        g: ink.g * ink.a + back.g * (1 - ink.a),
        b: ink.b * ink.a + back.b * (1 - ink.a),
      };
      const [light, dark] = [luminance(text), luminance(back)].sort((a, b) => b - a);
      const ratio = (light! + 0.05) / (dark! + 0.05);
      if (ratio < 4.5) {
        problems.push(
          `contrast ${ratio.toFixed(2)}: "${element.textContent!.trim().slice(0, 30)}" (${element.className})`,
        );
      }
    }

    for (const control of document.querySelectorAll<HTMLElement>(
      "button, a[href], input, select, textarea",
    )) {
      if (!visible(control)) continue;
      const labelled = control.getAttribute("aria-labelledby");
      const name =
        control.getAttribute("aria-label") ??
        (labelled ? document.getElementById(labelled)?.textContent : null) ??
        (control.id ? document.querySelector(`label[for="${control.id}"]`)?.textContent : null) ??
        control.closest("label")?.textContent ??
        (control.matches("button, a") ? spoken(control) : null);
      if (!name?.trim()) problems.push(`no name: ${control.outerHTML.slice(0, 80)}`);
      const option = control.getAttribute("role") === "option";
      if (control.tabIndex < 0 && !option) {
        problems.push(`not reachable: ${control.outerHTML.slice(0, 80)}`);
      }
    }
    return problems;
  });
}
