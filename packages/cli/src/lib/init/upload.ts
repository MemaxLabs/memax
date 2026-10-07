// Steps 5 and 6 of memax init: upload each space's statements as an import
// (at most 500 a request), then wait for the judge to look at every
// proposal and for the import's conflict check (plan 25 §7.3: under 20 s).
import type { V2 } from "memax-sdk";
import type { Upload } from "./scan.js";
import type { InitDeps } from "./types.js";

/** The most statements one import request takes (ledger.MaxImportItems). */
export const BATCH = 500;

export interface Uploaded {
  space: V2.Space;
  imports: V2.ImportResult[];
}

/** Uploads one space's statements; the files and skips go with the first batch. */
export async function upload(
  d: InitDeps,
  space: V2.Space,
  u: Upload,
  run: string,
): Promise<Uploaded> {
  const out: Uploaded = { space, imports: [] };
  if (u.items.length === 0 && u.skipped.length === 0 && u.files.length === 0)
    return out;
  const batches = Math.max(1, Math.ceil(u.items.length / BATCH));
  for (let b = 0; b < batches; b++) {
    const res = await d.memax.v2.imports.create(
      space.id,
      {
        client: `memax-cli ${d.version}`,
        session_ref: run,
        files: b === 0 ? u.files : [],
        skipped: b === 0 ? u.skipped : [],
        items: u.items.slice(b * BATCH, (b + 1) * BATCH),
      },
      // One key per run and batch: a retry of this request resumes it, and
      // the next run's statements are checked against the record instead.
      { idempotencyKey: `${run}:${space.id}:${b}`, via: "cli" },
    );
    out.imports.push(res);
  }
  return out;
}

export interface Judged {
  views: V2.ImportView[];
  ready: boolean;
  ms: number;
}

/** Polls the imports until each is ready, or the deadline passes. */
export async function waitForJudge(
  d: InitDeps,
  space: V2.Space,
  ids: string[],
  timeoutMs: number,
  onProgress?: (p: { working: number; proposals: number }) => void,
): Promise<Judged> {
  const start = d.now();
  let views: V2.ImportView[] = [];
  let delay = 250;
  for (;;) {
    views = await Promise.all(
      ids.map((id) => d.memax.v2.imports.get(space.id, id)),
    );
    const working = views.reduce((n, v) => n + v.progress.working, 0);
    const proposals = views.reduce((n, v) => n + v.progress.proposals, 0);
    onProgress?.({ working, proposals });
    if (views.every((v) => v.progress.ready))
      return { views, ready: true, ms: d.now() - start };
    if (d.now() - start >= timeoutMs)
      return { views, ready: false, ms: d.now() - start };
    await d.sleep(delay);
    delay = Math.min(1_000, delay * 2);
  }
}
