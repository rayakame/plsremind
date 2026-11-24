/** Leaves the dashboard for another address, in place of the page that sent the browser there. */
export function leave(url: string): void {
  window.location.replace(url);
}
