import { useState } from "react";

function initials(name: string, count: number): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  return words
    .slice(0, count)
    .map((word) => [...word][0]!.toUpperCase())
    .join("");
}

/** A picture of an account or an app, or its initials when there is none. */
export function Avatar(props: {
  name: string;
  src?: string | undefined;
  size?: "m" | "l";
  square?: boolean;
}) {
  const [broken, setBroken] = useState(false);
  const className = ["ava", props.size === "l" ? "l" : "", props.square ? "square" : ""]
    .filter(Boolean)
    .join(" ");
  if (props.src && !broken) {
    return <img className={className} src={props.src} alt="" onError={() => setBroken(true)} />;
  }
  return (
    <span className={className} aria-hidden="true">
      {initials(props.name, props.square ? 1 : 2)}
    </span>
  );
}
