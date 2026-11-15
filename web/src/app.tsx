import type { Client } from "plsremind";
import { useEffect } from "react";
import { Redirect, Route, Router, Switch, useLocation, useSearch } from "wouter";
import type { BaseLocationHook, BaseSearchHook } from "wouter";
import { useProfile } from "./hub/hooks.js";
import { readPrefs } from "./hub/prefs.js";
import { SessionProvider, useSession } from "./hub/session.js";
import { setLocale, useT } from "./i18n/index.js";
import { LoginPage, RegisterPage, SetupPage } from "./pages/account.js";
import { NoticesProvider } from "./shell/notices.js";
import { localPath, routes } from "./shell/router.js";
import { Shell } from "./shell/shell.js";

const themeKey = "plsremind.theme";

/** Applies the profile's language and theme to the document. */
function Preferences() {
  const profile = useProfile();
  const { theme } = readPrefs(profile.preferences);

  useEffect(() => {
    setLocale(profile.locale ?? navigator.language);
  }, [profile.locale]);

  useEffect(() => {
    const dark = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const resolved = theme === "system" ? (dark.matches ? "dark" : "light") : theme;
      document.documentElement.dataset.theme = resolved;
    };
    apply();
    try {
      if (theme === "system") localStorage.removeItem(themeKey);
      else localStorage.setItem(themeKey, theme);
    } catch {
      // The theme then only applies once the profile has loaded.
    }
    dark.addEventListener("change", apply);
    return () => dark.removeEventListener("change", apply);
  }, [theme]);

  return null;
}

function Language() {
  const t = useT();
  useEffect(() => {
    document.documentElement.lang = t("meta.lang");
  });
  return null;
}

function Message(props: { title: string; text?: string }) {
  return (
    <main className="center">
      <h1 className="h1">{props.title}</h1>
      {props.text && <p className="lead">{props.text}</p>}
    </main>
  );
}

function Placeholder(props: { title: string }) {
  return <h1 className="h1">{props.title}</h1>;
}

function SignedIn() {
  const t = useT();
  const [path] = useLocation();
  const search = new URLSearchParams(useSearch());
  if (path === routes.login || path === routes.register || path === routes.setup) {
    return <Redirect to={localPath(search.get("next"))} replace />;
  }
  return (
    <NoticesProvider>
      <Preferences />
      <Shell>
        <Switch>
          <Route path="/">
            <Placeholder title={t("nav.all")} />
          </Route>
          <Route path="/lists/:id">
            <Placeholder title={t("nav.lists")} />
          </Route>
          <Route path="/calendar">
            <Placeholder title={t("nav.calendar")} />
          </Route>
          <Route path="/done">
            <Placeholder title={t("nav.done")} />
          </Route>
          <Route path="/items/:id">
            <Placeholder title={t("nav.all")} />
          </Route>
          <Route path="/settings/:area?">
            <Placeholder title={t("nav.settings")} />
          </Route>
          <Route>
            <Placeholder title={t("common.notFound")} />
          </Route>
        </Switch>
      </Shell>
    </NoticesProvider>
  );
}

function Gate() {
  const t = useT();
  const state = useSession();
  const [path] = useLocation();
  const search = useSearch();

  useEffect(() => {
    if (state.phase !== "ready") setLocale(navigator.language);
  }, [state.phase]);

  if (state.phase === "loading") return <Message title={t("connection.loading")} />;
  if (state.phase === "unreachable") {
    return <Message title={t("connection.connecting")} text={t("connection.unreachable")} />;
  }
  if (state.phase === "setup") {
    if (path !== routes.setup) return <Redirect to={routes.setup} replace />;
    return <SetupPage info={state.info} rest={state.rest} />;
  }
  if (state.phase === "signedOut") {
    if (path === routes.register) return <RegisterPage info={state.info} rest={state.rest} />;
    if (path !== routes.login) {
      const error = new URLSearchParams(search).get("auth_error");
      const target = path + (search && !error ? `?${search}` : "");
      const query = new URLSearchParams();
      if (error) query.set("auth_error", error);
      else if (target !== "/") query.set("next", target);
      const tail = query.toString();
      return <Redirect to={routes.login + (tail ? `?${tail}` : "")} replace />;
    }
    return <LoginPage info={state.info} rest={state.rest} />;
  }
  return <SignedIn />;
}

export interface AppLocation {
  hook: BaseLocationHook;
  searchHook?: BaseSearchHook;
}

export function App(props: { make?: () => Client; location?: AppLocation }) {
  return (
    <SessionProvider make={props.make}>
      <Language />
      <Router {...(props.location ?? {})}>
        <Gate />
      </Router>
    </SessionProvider>
  );
}
