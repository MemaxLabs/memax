/**
 * The seal line under Activity's "This week": how far the space's
 * receipts are sealed into the signed hash chain (plan §5.3), whether
 * its checkpoints are signed, and what the last check from the first
 * receipt found. Pure, catalogue in and sentences out, so both locales
 * are unit-tested. No board draws it: a quiet line, said plainly.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import { count, formatWhen, type AppCopy } from "../copy";
import type { SealView } from "../data/activity";
import { formatCount } from "../reads-copy";
import type { ActivityCopy } from "./sentence";

export interface SealSentence {
  text: string;
  /** The check found the chain changed: the one line said in vermilion. */
  problem?: boolean;
}

export function sealSentences(
  copy: ActivityCopy,
  app: AppCopy,
  seal: SealView,
  { now, timeZone, locale }: { now: Date; timeZone: string; locale: Locale },
): SealSentence[] {
  const s = copy.seal;
  const when = (iso: string) => formatWhen(app, iso, now, timeZone, locale);
  const out: SealSentence[] = [];
  if (seal.sealed > 0) {
    const n = formatCount(seal.sealed, locale);
    out.push({
      text: seal.sealedAt
        ? interpolate(s.through, { n, when: when(seal.sealedAt) })
        : interpolate(s.throughPlain, { n }),
    });
    if (seal.signed === false) out.push({ text: s.unsigned });
  } else if (seal.unsealed > 0) {
    out.push({
      text: count(s.waitingOne, s.waiting, seal.unsealed, {
        n: formatCount(seal.unsealed, locale),
      }),
    });
  }
  if (seal.verified) {
    const at = when(seal.verified.at);
    const problems = seal.verified.problems;
    out.push(
      problems === 0
        ? { text: interpolate(s.verified, { when: at }) }
        : {
            text: count(s.problemsOne, s.problems, problems, { when: at }),
            problem: true,
          },
    );
  }
  return out;
}
