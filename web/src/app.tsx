import type { Client } from "plsremind";
import { lazy, Suspense, useEffect, useRef } from "react";
import { Redirect, Route, Router, Switch, useLocation, useSearch } from "wouter";
import type { BaseLocationHook, BaseSearchHook } from "wouter";
import { BarProvider } from "./bar/context.js";
import { Keys } from "./keys/keys.js";
import { useProfile } from "./hub/hooks.js";
import { readPrefs } from "./hub/prefs.js";
import { SessionProvider, useSession } from "./hub/session.js";
import { describeCode } from "./hub/errors.js";
import { setLocale, t, useT } from "./i18n/index.js";
import { LoginPage, RegisterPage, SetupPage, takeNext } from "./pages/account.js";
import { DonePage } from "./pages/done.js";
import { FindPage } from "./pages/find.js";
import { ItemPage } from "./pages/item.js";
import { AllItemsPage, ListPage } from "./pages/items.js";
import { Details } from "./shell/details.js";
import { NotFound } from "./pages/not-found.js";
import { SelectionProvider } from "./shell/selection.js";
import { Boundary } from "./shell/boundary.js";
import { NoticesProvider, useNotices } from "./shell/notices.js";
import { localPath, routes } from "./shell/router.js";
import { Shell } from "./shell/shell.js";

const CalendarPage = lazy(() => import("./pages/calendar.js"));

const SettingsPage = lazy(() => import("./pages/settings/index.js"));

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

/** Says why linking an account elsewhere failed, once, and tidies the address. */
function AuthError() {
  const { notify } = useNotices();
  const [path, navigate] = useLocation();
  const search = useSearch();
  const code = new URLSearchParams(search).get("auth_error");
  const told = useRef<string | null>(null);
  useEffect(() => {
    if (!code || told.current === code) return;
    told.current = code;
    notify(describeCode(code) ?? t("errors.unexpected"), "danger");
    navigate(path, { replace: true });
  }, [code, notify, navigate, path]);
  return null;
}

function SignedIn() {
  const [path, navigate] = useLocation();
  const search = new URLSearchParams(useSearch());
  useEffect(() => {
    const next = takeNext();
    if (next && localPath(next) !== "/") navigate(localPath(next), { replace: true });
  }, [navigate]);
  if (path === routes.login || path === routes.register || path === routes.setup) {
    return <Redirect to={localPath(search.get("next"))} replace />;
  }
  return (
    <NoticesProvider>
      <Preferences />
      <SelectionProvider>
        <BarProvider>
          <Keys />
          <AuthError />
          <Shell panel={<Details />}>
            <Boundary>
              <Switch>
                <Route path="/">
                  <AllItemsPage />
                </Route>
                <Route path="/lists/:id">{(params) => <ListPage id={params.id} />}</Route>
                <Route path="/find">
                  <FindPage />
                </Route>
                <Route path="/calendar">
                  <Suspense fallback={null}>
                    <CalendarPage />
                  </Suspense>
                </Route>
                <Route path="/done">
                  <DonePage />
                </Route>
                <Route path="/items/:id">{(params) => <ItemPage id={params.id} />}</Route>
                <Route path="/settings/:area?/:section?">
                  {(params) => (
                    <Suspense fallback={null}>
                      <SettingsPage area={params.area} section={params.section} />
                    </Suspense>
                  )}
                </Route>
                <Route>
                  <NotFound />
                </Route>
              </Switch>
            </Boundary>
          </Shell>
        </BarProvider>
      </SelectionProvider>
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
      else if (target !== "/" && !state.asked) query.set("next", target);
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
