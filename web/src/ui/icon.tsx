const paths = {
  plus: "M10 4v12M4 10h12",
  menu: "M3 5h14M3 10h14M3 15h14",
  close: "M5 5l10 10M15 5L5 15",
  left: "M12 4l-6 6 6 6",
  right: "M8 4l6 6-6 6",
  up: "M4 12l6-6 6 6",
  down: "M4 8l6 6 6-6",
  search: "M9 15A6 6 0 1 0 9 3a6 6 0 0 0 0 12zM17 17l-3.6-3.6",
  open: "M8 4H4v12h12v-4M11 3h6v6M17 3l-8 8",
  trash: "M4 6h12M8 6V4h4v2M6 6l1 11h6l1-11",
  copy: "M7 7V3h10v10h-4M3 7h10v10H3z",
  check: "M4 10.5l4 4 8-9",
  warn: "M10 3l8 14H2L10 3zM10 8v4M10 14.5v.5",
} as const;

export type IconName = keyof typeof paths;

/** A line icon. With a label it stands for something on its own, otherwise it only decorates. */
export function Icon({
  name,
  label,
  className,
}: {
  name: IconName;
  label?: string;
  className?: string;
}) {
  return (
    <svg
      className={className ? `icon-svg ${className}` : "icon-svg"}
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    >
      <path d={paths[name]} />
    </svg>
  );
}
