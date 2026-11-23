import {
  Children,
  cloneElement,
  isValidElement,
  useId,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type Ref,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";

function classes(...names: (string | false | undefined)[]): string {
  return names.filter(Boolean).join(" ");
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "ghost" | "icon" | "danger" | undefined;
  ref?: Ref<HTMLButtonElement> | undefined;
}

export function Button({ variant, className, type = "button", ...rest }: ButtonProps) {
  const shape = variant === "icon" ? "ghost icon" : variant;
  return <button type={type} className={classes("btn", shape, className)} {...rest} />;
}

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  look?: "plain" | "title" | undefined;
}

export function Input({ look, className, ...rest }: InputProps) {
  const shape = look === "title" ? "plain title" : look;
  return <input className={classes("input", shape, className)} {...rest} />;
}

export function TextArea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={classes("input", className)} {...rest} />;
}

export function Select({ className, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={classes("input", className)} {...rest} />;
}

/** A label above one control. The error, if any, is announced with the control. */
export function Field(props: { label: string; error?: string | undefined; children: ReactNode }) {
  const id = useId();
  const control = Children.only(props.children);
  return (
    <div className="field">
      <label className="label" htmlFor={id}>
        {props.label}
      </label>
      {isValidElement<Record<string, unknown>>(control)
        ? cloneElement(control, {
            id,
            "aria-invalid": props.error ? "true" : undefined,
            "aria-describedby": props.error ? `${id}-error` : undefined,
          })
        : control}
      {props.error && (
        <span className="error" id={`${id}-error`}>
          {props.error}
        </span>
      )}
    </div>
  );
}

export function Toggle(props: {
  label: ReactNode;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean | undefined;
}) {
  return (
    <label className="toggle">
      <span>{props.label}</span>
      <input
        type="checkbox"
        className="check"
        checked={props.checked}
        disabled={props.disabled}
        onChange={(event) => props.onChange(event.target.checked)}
      />
    </label>
  );
}

export function Segmented<T extends string>(props: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  disabled?: boolean | undefined;
  describedBy?: string | undefined;
}) {
  return (
    <div className="seg" role="group" aria-label={props.label} aria-describedby={props.describedBy}>
      {props.options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={option.value === props.value}
          disabled={props.disabled}
          onClick={() => props.onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
