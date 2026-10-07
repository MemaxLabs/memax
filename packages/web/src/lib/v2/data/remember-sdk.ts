import { MemaxError } from "memax-sdk";
import type { Memax, V2 } from "memax-sdk";
import type { RememberCheck, Section, SpaceSummary } from "./types";

type V2Client = Pick<Memax, "v2">;

/** Remember's sections; open questions aren't offered there. */
const REMEMBER_SECTIONS: readonly Section[] = [
  "decisions",
  "conventions",
  "preferences",
];

const NOTHING: RememberCheck = {
  duplicate: null,
  section: null,
  condition: null,
};

/**
 * Remember's near-duplicate check over memax.v2 (plan §5.8): the nearest
 * kept memory or pending proposal the draft repeats, with who wrote it,
 * and its section as the suggestion. The check is advice: when it fails
 * (rate-limited, offline, the server down) Remember shows nothing and
 * Keep goes ahead. An aborted check (the person typed on) still throws,
 * so its answer is never applied to newer words.
 */
export async function checkRememberOver(
  client: V2Client,
  {
    space,
    statement,
    signal,
  }: { space: SpaceSummary; statement: string; signal?: AbortSignal },
): Promise<RememberCheck> {
  const text = statement.trim();
  if (!text) return NOTHING;
  let found: V2.NearDuplicates;
  try {
    found = await client.v2.memories.nearDuplicates(
      space.slug,
      { statement: text, limit: 1 },
      { signal },
    );
  } catch (err) {
    if (signal?.aborted) throw err;
    if (err instanceof MemaxError || err instanceof TypeError) return NOTHING;
    throw err;
  }
  return rememberCheckOf(found);
}

/** The check, from the API's answer. */
export function rememberCheckOf(found: V2.NearDuplicates): RememberCheck {
  const top = found.items[0];
  if (!top) return NOTHING;
  const section = REMEMBER_SECTIONS.includes(top.memory.section)
    ? top.memory.section
    : null;
  return {
    duplicate: {
      ref: top.memory.ref,
      lifecycle: top.memory.lifecycle === "proposed" ? "proposed" : "kept",
      agent:
        top.created.actor_kind === "agent" ? (top.created.agent ?? null) : null,
      writtenAt: top.created.occurred_at,
      match: top.match,
    },
    section,
    // PLACEHOLDER: a memory's conditions are typed predicates (plan §5.9),
    // and the judge proposes none by default; they aren't worded yet.
    condition: null,
  };
}
