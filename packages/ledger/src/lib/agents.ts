/** Where an agent runs. Shown after its name when a stamp asks for its surface. */
export type AgentSurface = "cli" | "ide" | "cloud" | "chat" | "memax";

/** Outlined monogram = an agent, solid ink = a person, night = Dream. */
export type AgentKind = "agent" | "dream" | "person";

export interface AgentInfo {
  /** Fixed-width monogram, two or three characters. */
  mono: string;
  /** Display name. Agent names are proper nouns and are never translated. */
  name: string;
  /** A known surface (translated), or a literal label for an agent Memax doesn't ship. */
  surface: AgentSurface | (string & {});
  kind?: AgentKind;
}

/**
 * The agents Memax ships with. Monograms only, never a vendor's logo or colour.
 * Apps can add to this through `LedgerProvider agents`; any other key still
 * renders, with a monogram derived from its name.
 */
export const AGENTS = {
  "claude-code": { mono: "CC", name: "Claude Code", surface: "cli" },
  codex: { mono: "CX", name: "Codex", surface: "cloud" },
  cursor: { mono: "CU", name: "Cursor", surface: "ide" },
  chatgpt: { mono: "GPT", name: "ChatGPT", surface: "chat" },
  claude: { mono: "CL", name: "Claude", surface: "chat" },
  gemini: { mono: "GM", name: "Gemini CLI", surface: "cli" },
  copilot: { mono: "CP", name: "Copilot", surface: "ide" },
  opencode: { mono: "OC", name: "OpenCode", surface: "cli" },
  dream: { mono: "DR", name: "Dream", surface: "memax", kind: "dream" },
} as const satisfies Record<string, AgentInfo>;

/** A key of the shipped registry. Any string is accepted wherever an agent is expected. */
export type KnownAgent = keyof typeof AGENTS;

/** The result of looking up who did something. `surface` is a surface key or a literal label. */
export interface ResolvedAgent {
  mono: string;
  name: string;
  surface: AgentSurface | "person" | "agent" | (string & {});
  kind: AgentKind;
}

export interface AgentQuery {
  /** A registry key (`codex`) or any other agent identifier. */
  agent?: string | undefined;
  /** A person's initials. Takes precedence over `agent`. */
  person?: string | undefined;
  /** Overrides the display name. */
  name?: string | undefined;
}

const WORD_SPLIT = /[\s\-_./]+/u;
const NON_ALNUM = /[^\p{L}\p{N}]/gu;

/**
 * Derives a two-character monogram for an agent Memax doesn't ship: the
 * initials of the first two words, or the first two characters of one word.
 * "AI" is never produced (the brand never says AI), so "aider" becomes "AD".
 */
export function monogramFor(label: string): string {
  const words = label
    .split(WORD_SPLIT)
    .map((w) => w.replace(NON_ALNUM, ""))
    .filter(Boolean);
  let mono =
    words.length >= 2
      ? [...words[0]!].slice(0, 1).join("") +
        [...words[1]!].slice(0, 1).join("")
      : [...(words[0] ?? "")].slice(0, 2).join("");
  mono = mono.toUpperCase();
  if (mono === "AI") {
    const rest = (
      words
        .join("")
        .slice(2)
        .match(/[^aeiou\d]/i)?.[0] ?? "G"
    ).toUpperCase();
    mono = `A${rest}`;
  }
  return mono || "AG";
}

/** Looks up an agent or a person. Unknown agents resolve gracefully. */
export function resolveAgent(
  registry: Readonly<Record<string, AgentInfo>>,
  { agent, person, name }: AgentQuery,
  fallbackName = "Agent",
): ResolvedAgent {
  if (person) {
    return {
      mono: person.toUpperCase().slice(0, 3),
      name: name || person,
      surface: "person",
      kind: "person",
    };
  }
  const known = agent ? registry[agent] : undefined;
  if (known) {
    return {
      mono: known.mono,
      name: name || known.name,
      surface: known.surface,
      kind: known.kind ?? "agent",
    };
  }
  const label = name || agent || fallbackName;
  return {
    mono: monogramFor(label),
    name: label,
    surface: "agent",
    kind: "agent",
  };
}
