import { cx } from "../lib/cx";

const ICONS = {
  search: ["M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14z", "m20 20-4-4"],
  check: ["M20 6 9 17l-5-5"],
  x: ["M18 6 6 18", "M6 6l12 12"],
  pencil: ["M4 20h4L19 9l-4-4L4 16v4z", "m13 7 4 4"],
  "arrow-right": ["M5 12h14", "m13 6 6 6-6 6"],
  "arrow-up-right": ["M7 17 17 7", "M8 7h9v9"],
  enter: ["M9 10l-5 5 5 5", "M20 4v7a4 4 0 0 1-4 4H4"],
  clock: ["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z", "M12 7v5l3 2"],
  file: [
    "M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z",
    "M14 3v5h5",
  ],
  terminal: ["m4 17 6-5-6-5", "M12 19h8"],
  plus: ["M12 5v14", "M5 12h14"],
  "chevron-down": ["m6 9 6 6 6-6"],
  "chevron-right": ["m9 6 6 6-6 6"],
  today: [
    "M5 5h14a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z",
    "M4 10h16",
    "M8 3v4",
    "M16 3v4",
  ],
  review: [
    "M3 13h5l2 3h4l2-3h5",
    "M5.5 5h13L21 13v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6z",
  ],
  brief: [
    "M6 3h9l4 4v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z",
    "M9 11h7",
    "M9 15h7",
    "M9 7h3",
  ],
  memories: [
    "M9 6h11",
    "M9 12h11",
    "M9 18h11",
    "M4.5 6h.01",
    "M4.5 12h.01",
    "M4.5 18h.01",
  ],
  handoff: ["M4 8h14", "m14 4 4 4-4 4", "M20 16H6", "m10 12-4 4 4 4"],
  agents: [
    "M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z",
    "M12 3.5v4",
    "M12 16.5v4",
    "M4.6 7.75l3.5 2",
    "M15.9 14.25l3.5 2",
    "M4.6 16.25l3.5-2",
    "M15.9 9.75l3.5-2",
  ],
  settings: [
    "M4 7h10",
    "M18 7h2",
    "M4 17h4",
    "M12 17h8",
    "M16 5v4",
    "M10 15v4",
  ],
  alert: ["M12 4 2.5 20h19z", "M12 10v4", "M12 17h.01"],
  shield: [
    "M12 21s7-3.5 7-9V6l-7-3-7 3v6c0 5.5 7 9 7 9z",
    "M12 8v5",
    "M12 16h.01",
  ],
  sync: [
    "M20 12a8 8 0 0 1-13.7 5.6",
    "M4 12a8 8 0 0 1 13.7-5.6",
    "M18 3v4h-4",
    "M6 21v-4h4",
  ],
  link: [
    "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1",
    "M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1",
  ],
  commit: ["M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z", "M3 12h6", "M15 12h6"],
  globe: [
    "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z",
    "M3 12h18",
    "M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z",
  ],
  chat: ["M20 15a2 2 0 0 1-2 2H8l-4 4V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2z"],
  command: [
    "M9 9V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V9z",
  ],
  forget: ["M4 10h16v4H4z", "M8 7V5", "M16 19v-2"],
  receipt: ["M6 3h12v18l-3-2-3 2-3-2-3 2z", "M9 8h6", "M9 12h6"],
  copy: [
    "M9 9h10a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H10a1 1 0 0 1-1-1z",
    "M5 15V5a1 1 0 0 1 1-1h9",
  ],
  space: [
    "M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z",
    "M4 10h16",
  ],
} as const satisfies Record<string, readonly string[]>;

/** A name accepted by `Icon`. */
export type IconName = keyof typeof ICONS;

/** Every icon name, in the handoff's order. */
export const ICON_NAMES = Object.keys(ICONS) as IconName[];

export interface IconProps {
  name: IconName;
  /** 16 in rows, buttons and the rail; 18–20 in the command bar and empty states. */
  size?: number;
  className?: string;
  /** Gives the icon an accessible name. Without it the icon is decorative. */
  title?: string;
}

/** Line icons on a 24px grid, 1.5px stroke, drawn in currentColor. */
export function Icon({ name, size = 16, className, title }: IconProps) {
  const paths: readonly string[] = ICONS[name] ?? ICONS.memories;
  return (
    <svg
      className={cx("mx-icon", className)}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden={title ? undefined : true}
      role={title ? "img" : undefined}
    >
      {title ? <title>{title}</title> : null}
      {paths.map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  );
}
