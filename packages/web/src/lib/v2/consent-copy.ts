/**
 * OAuthConsent's sentences from data (lib/v2/data/consent.ts): a space's
 * meta line and file, the footnote for the level the agent gets. Pure, so
 * both locales are unit-tested.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Translations } from "@/i18n/locales/en";
import { count } from "./copy";
import type { ConsentSpaceView } from "./data/consent";

export type ConsentCopy = Translations["ledger"]["consent"];

/** "Project · 214 memories", "Team · 2 people", "Just you"; V1 says so. */
export function spaceMetaLine(
  copy: ConsentCopy,
  space: ConsentSpaceView,
): string {
  const m = copy.meta;
  let meta: string;
  switch (space.kind) {
    case "personal":
      meta = m.personal;
      break;
    case "team":
      meta =
        space.people === null
          ? m.team
          : interpolate(m.teamMeta, {
              people: count(m.peopleOne, m.people, space.people),
            });
      break;
    default:
      meta =
        space.memories === null
          ? m.project
          : interpolate(m.projectMeta, {
              memories: count(m.memoriesOne, m.memories, space.memories),
            });
  }
  return space.onV2 ? meta : interpolate(m.onV1, { meta });
}

/** The line under Allow, for the level the agent is connected at. */
export function consentFootnote(
  copy: ConsentCopy,
  client: string,
  space: ConsentSpaceView | undefined,
): string {
  if (space && !space.onV2) return interpolate(copy.footnoteV1, { client });
  if (space?.autonomy === "read") {
    return interpolate(copy.footnoteRead, { client });
  }
  return interpolate(copy.footnote, { client });
}
