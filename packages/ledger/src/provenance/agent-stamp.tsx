import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";

export interface AgentStampProps {
  /** A registry key (`claude-code`, `codex`, `dream`…) or any other agent; unknown ones get a derived monogram. */
  agent?: string;
  /** A person's initials ("ZZ"). Takes precedence over `agent`. */
  person?: string;
  /** Display name; for a person, their name ("Jiahao", or the locale's "You"). */
  name?: string;
  /** Show the name beside the stamp. */
  showName?: boolean;
  /** With `showName`, add the surface (CLI, IDE, Cloud, Chat). */
  surface?: boolean;
  size?: "sm" | "md";
  /**
   * Hide the stamp from assistive technology because the actor's name is
   * already written next to it ("Codex is waiting on you").
   */
  decorative?: boolean;
  className?: string;
}

/**
 * Who did it, as a fixed-width monogram: outlined for an agent, solid ink for
 * a person, `night` for Dream. Never a vendor's logo or colour. Assistive
 * technology hears the name, not the letters.
 */
export function AgentStamp({
  agent,
  person,
  name,
  showName = false,
  surface = false,
  size = "md",
  decorative = false,
  className,
}: AgentStampProps) {
  const { agents, strings } = useLedger();
  const who = resolveAgent(
    agents,
    { agent, person, name },
    strings.agent.fallback,
  );
  const surfaceLabel =
    (strings.surface as Record<string, string | undefined>)[who.surface] ??
    who.surface;
  return (
    <span
      className={cx("mx-stamp-wrap", className)}
      aria-hidden={decorative || undefined}
    >
      <span
        className={cx(
          "mx-stamp",
          `mx-stamp--${who.kind}`,
          `mx-stamp--${size}`,
          who.mono.length > 2 && "is-wide",
        )}
        title={who.name}
        aria-hidden="true"
      >
        {who.mono}
      </span>
      {showName ? (
        <span className="mx-stamp-name">
          {who.name}
          {surface ? (
            <span className="mx-stamp-surface">{surfaceLabel}</span>
          ) : null}
        </span>
      ) : decorative ? null : (
        <span className="mx-sr">{who.name}</span>
      )}
    </span>
  );
}
