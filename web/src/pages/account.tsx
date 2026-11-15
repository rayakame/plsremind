import type { Info, RestClient } from "plsremind";
import { useState, type FormEvent, type ReactNode } from "react";
import { Link, useSearch } from "wouter";
import { describeCode, describeError } from "../hub/errors.js";
import { useSessionStore } from "../hub/session.js";
import { locale, useT } from "../i18n/index.js";
import { routes } from "../shell/router.js";
import { Button, Field, Input } from "../ui/controls.js";
import { Notice } from "../ui/layout.js";

function Frame(props: {
  title: string;
  lead?: string;
  error: string | undefined;
  children: ReactNode;
}) {
  return (
    <main className="center">
      <h1 className="h1">{props.title}</h1>
      {props.lead && <p className="lead">{props.lead}</p>}
      {props.error && (
        <Notice tone="danger" flat>
          {props.error}
        </Notice>
      )}
      {props.children}
    </main>
  );
}

function useSubmit(work: () => Promise<unknown>) {
  const session = useSessionStore();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await work();
      rememberNext(null);
      session.reload();
    } catch (failure) {
      setError(describeError(failure));
    }
    setBusy(false);
  };
  return { error, busy, submit };
}

function browserAccount() {
  return {
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    locale: locale(),
  };
}

const nextKey = "plsremind.next";
// A sign-in elsewhere that was abandoned must not steer a later one.
const nextFor = 10 * 60_000;

// A sign-in elsewhere always returns to the start page, so the wanted one is kept here.
function rememberNext(next: string | null): void {
  try {
    if (next) sessionStorage.setItem(nextKey, `${Date.now()} ${next}`);
    else sessionStorage.removeItem(nextKey);
  } catch {
    // The start page then opens after signing in.
  }
}

/** The page remembered before a sign-in elsewhere, once. */
export function takeNext(): string | null {
  try {
    const stored = sessionStorage.getItem(nextKey);
    sessionStorage.removeItem(nextKey);
    if (!stored) return null;
    const at = stored.indexOf(" ");
    const fresh = Date.now() - Number(stored.slice(0, at)) < nextFor;
    return fresh ? stored.slice(at + 1) : null;
  } catch {
    return null;
  }
}

function Providers(props: { info: Info; rest: RestClient; inviteCode?: string }) {
  const t = useT();
  const next = new URLSearchParams(useSearch()).get("next");
  const others = props.info.loginMethods.filter((method) => method.type !== "password");
  if (others.length === 0) return null;
  return (
    <div className="stack">
      {others.map((method) => (
        <a
          key={method.key}
          className="btn"
          href={props.rest.oauthStartUrl(
            method.key,
            props.inviteCode ? { inviteCode: props.inviteCode } : {},
          )}
          onClick={() => rememberNext(next)}
        >
          {t("account.continueWith", { name: method.name })}
        </a>
      ))}
    </div>
  );
}

const hasPassword = (info: Info) => info.loginMethods.some((method) => method.type === "password");

export function LoginPage(props: { info: Info; rest: RestClient }) {
  const t = useT();
  const search = new URLSearchParams(useSearch());
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, busy, submit } = useSubmit(() => props.rest.login({ username, password }));
  const elsewhere = search.get("auth_error");
  const shown =
    error ?? (elsewhere ? (describeCode(elsewhere) ?? t("errors.unexpected")) : undefined);

  return (
    <Frame title={t("account.signIn")} lead={props.info.name} error={shown}>
      {hasPassword(props.info) && (
        <form className="stack" onSubmit={(event) => void submit(event)}>
          <Field label={t("account.username")}>
            <Input
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="username"
              autoCapitalize="none"
              required
              autoFocus
            />
          </Field>
          <Field label={t("account.password")}>
            <Input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>
          <Button variant="primary" type="submit" disabled={busy}>
            {t("account.signIn")}
          </Button>
        </form>
      )}
      <Providers info={props.info} rest={props.rest} />
      <p className="text">
        <Link href={routes.register}>{t("account.toRegister")}</Link>
      </p>
    </Frame>
  );
}

export function RegisterPage(props: { info: Info; rest: RestClient }) {
  const t = useT();
  const invited = props.info.registration === "invite";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const { error, busy, submit } = useSubmit(() =>
    props.rest.register({
      username,
      password,
      ...(invited ? { inviteCode } : {}),
      ...browserAccount(),
    }),
  );

  return (
    <Frame title={t("account.register")} lead={props.info.name} error={error}>
      <form className="stack" onSubmit={(event) => void submit(event)}>
        {invited && (
          <Field label={t("account.inviteCode")}>
            <Input
              value={inviteCode}
              onChange={(event) => setInviteCode(event.target.value)}
              autoComplete="off"
              required
            />
          </Field>
        )}
        {hasPassword(props.info) && (
          <>
            <Field label={t("account.username")}>
              <Input
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                autoComplete="username"
                autoCapitalize="none"
                required
              />
            </Field>
            <Field label={t("account.password")}>
              <Input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="new-password"
                required
              />
            </Field>
            <Button variant="primary" type="submit" disabled={busy}>
              {t("account.register")}
            </Button>
          </>
        )}
      </form>
      <Providers info={props.info} rest={props.rest} inviteCode={invited ? inviteCode : ""} />
      <p className="text">
        <Link href={routes.login}>{t("account.toLogin")}</Link>
      </p>
    </Frame>
  );
}

export function SetupPage(props: { info: Info; rest: RestClient }) {
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, busy, submit } = useSubmit(() =>
    props.rest.setup({ username, password, ...browserAccount() }),
  );

  return (
    <Frame
      title={t("account.setup", { name: props.info.name })}
      lead={t("account.setupLead")}
      error={error}
    >
      <form className="stack" onSubmit={(event) => void submit(event)}>
        <Field label={t("account.username")}>
          <Input
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            required
            autoFocus
          />
        </Field>
        <Field label={t("account.password")}>
          <Input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
            required
          />
        </Field>
        <Button variant="primary" type="submit" disabled={busy}>
          {t("account.create")}
        </Button>
      </form>
    </Frame>
  );
}
