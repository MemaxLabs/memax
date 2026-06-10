"use client";

export function EditorField({
  label,
  value,
  onChange,
  rows,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
  rows: number;
}) {
  return (
    <label className="block">
      <span className="mb-2 block text-[12px] font-medium text-fg-3">
        {label}
      </span>
      <textarea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        rows={rows}
        className="w-full rounded-surface border border-border/50 bg-surface-1 px-3 py-3 font-mono text-[12px] leading-5 text-fg-2 outline-none focus:border-fg-3"
      />
    </label>
  );
}

export function ModeButton({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-lg px-3 py-1.5 text-[12px] font-medium transition-colors ${
        active
          ? "bg-surface-2 text-fg-1"
          : "text-fg-4 hover:bg-surface-1 hover:text-fg-2"
      }`}
    >
      {label}
    </button>
  );
}
