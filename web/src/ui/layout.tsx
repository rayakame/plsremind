import type { AnchorHTMLAttributes, HTMLAttributes, ReactNode } from "react";

export function Rows(props: HTMLAttributes<HTMLDivElement>) {
  return <div {...props} className={`rows ${props.className ?? ""}`.trim()} />;
}

export interface RowProps extends HTMLAttributes<HTMLDivElement> {
  selected?: boolean | undefined;
  done?: boolean | undefined;
  tall?: boolean | undefined;
}

export function Row({ selected, done, tall, className, ...rest }: RowProps) {
  const names = ["row", selected && "sel", done && "done", tall && "tall", className];
  return <div className={names.filter(Boolean).join(" ")} {...rest} />;
}

export function Tag(props: { tone?: "high" | undefined; children: ReactNode }) {
  return <span className={props.tone ? `tag ${props.tone}` : "tag"}>{props.children}</span>;
}

export function Kbd(props: { children: ReactNode }) {
  return <kbd className="kbd">{props.children}</kbd>;
}

export interface NavItemProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  active?: boolean | undefined;
  count?: number | undefined;
}

export function NavItem({ active, count, children, ...rest }: NavItemProps) {
  return (
    <a className={active ? "nav on" : "nav"} aria-current={active ? "page" : undefined} {...rest}>
      <span className="t">{children}</span>
      {count !== undefined && <span className="count">{count}</span>}
    </a>
  );
}

export function Card(props: { title?: string | undefined; children: ReactNode }) {
  return (
    <section className="card" aria-label={props.title}>
      {props.title && <h3 className="label">{props.title}</h3>}
      {props.children}
    </section>
  );
}

export function Notice(props: {
  tone?: "danger" | undefined;
  flat?: boolean | undefined;
  children: ReactNode;
  action?: ReactNode;
}) {
  const names = ["notice", props.tone, props.flat && "flat"].filter(Boolean).join(" ");
  return (
    <div className={names} role={props.tone === "danger" ? "alert" : "status"}>
      <span className="t">{props.children}</span>
      {props.action}
    </div>
  );
}
