export const routes = {
  items: "/",
  list: (id: string) => `/lists/${encodeURIComponent(id)}`,
  calendar: "/calendar",
  done: "/done",
  item: (id: string) => `/items/${encodeURIComponent(id)}`,
  settings: (area = "personal") => `/settings/${area}`,
  login: "/login",
  register: "/register",
  setup: "/setup",
} as const;

/** The path to go to after signing in, if it stays inside the dashboard. */
export function localPath(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) return "/";
  return next;
}
