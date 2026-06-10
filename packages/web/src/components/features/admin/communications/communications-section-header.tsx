"use client";

export function CommunicationsSectionHeader({
  eyebrow,
  title,
  description,
  trailing,
}: {
  eyebrow?: string;
  title: string;
  description?: string;
  trailing?: React.ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        {eyebrow ? (
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-[0.08em] text-fg-3">
            {eyebrow}
          </p>
        ) : null}
        <h2 className="text-[18px] font-semibold tracking-tight text-fg-1">
          {title}
        </h2>
        {description ? (
          <p className="mt-1 max-w-3xl text-[13.5px] leading-relaxed text-fg-2">
            {description}
          </p>
        ) : null}
      </div>
      {trailing ? <div className="shrink-0">{trailing}</div> : null}
    </div>
  );
}
