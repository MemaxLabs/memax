// Step 4 of memax init, all on this machine: read each agent file, split
// it into statements, scan them for secrets and hidden characters, and
// decide each one's trust. What comes out is the import requests (one for
// the project space, one for Personal) and what stays behind.
import { readFileSync } from "node:fs";
import type { V2 } from "memax-sdk";
import type { FoundFile } from "./files.js";
import { baseLines, onBase, type BranchContext, type Git } from "./git.js";
import {
  addHidden,
  emptyHidden,
  totalHidden,
  type HiddenCounts,
} from "./hidden.js";
import { scanSecrets, secretsIn } from "./secrets.js";
import { splitMarkdown } from "./split.js";

export interface ScannedFile {
  file: FoundFile;
  /** Statements found, sent, and kept on the machine (secrets, too long, over the limit). */
  statements: number;
  sent: number;
  skipped: number;
  hidden: HiddenCounts;
  /** Memax compiled it: not read again. */
  compiled: boolean;
  /** Statements only on another branch than the default one: sent as external. */
  branchOnly: number;
  /** Secrets in lines that aren't statements (code blocks): never read, but worth a look. */
  otherSecrets: number;
  unreadable?: boolean;
}

export interface Upload {
  files: V2.ImportFile[];
  skipped: V2.ImportSkip[];
  items: V2.ImportItemInput[];
}

export interface ScanResult {
  project: Upload;
  personal: Upload;
  files: ScannedFile[];
  hidden: HiddenCounts;
}

export interface ScanOptions {
  root: string | null;
  git: Git;
  branch: BranchContext | null;
}

const URL_IN_TEXT = /\bhttps?:\/\/[^\s<>()[\]"'`]+/g;

function emptyUpload(): Upload {
  return { files: [], skipped: [], items: [] };
}

/** Reads and splits every file, and builds the uploads. */
export async function scanFiles(
  found: FoundFile[],
  o: ScanOptions,
): Promise<ScanResult> {
  const out: ScanResult = {
    project: emptyUpload(),
    personal: emptyUpload(),
    files: [],
    hidden: emptyHidden(),
  };
  for (const [fi, file] of found.entries()) {
    const upload = file.location === "repository" ? out.project : out.personal;
    const scanned: ScannedFile = {
      file,
      statements: 0,
      sent: 0,
      skipped: 0,
      hidden: emptyHidden(),
      compiled: false,
      branchOnly: 0,
      otherSecrets: 0,
    };
    out.files.push(scanned);
    let content: string;
    try {
      content = readFileSync(file.path, "utf8");
    } catch {
      scanned.unreadable = true;
      continue;
    }
    if (content.includes("\u{FFFD}")) {
      // Not text Memax can read as notes (or not UTF-8): left alone.
      scanned.unreadable = true;
      continue;
    }
    const split = splitMarkdown(content, file.location);
    scanned.hidden = split.hidden;
    addHidden(out.hidden, split.hidden);
    if (split.compiled) {
      scanned.compiled = true;
      continue;
    }
    const hits = await scanSecrets(content, file.label);
    const lines =
      file.location === "repository" && o.root && o.branch && file.repoPath
        ? baseLines(o.branch, o.root, file.repoPath, o.git)
        : null;
    const used = new Set<number>();
    scanned.statements =
      split.statements.length + split.tooLong.length + split.overLimit.length;
    split.statements.forEach((st, si) => {
      const ref = `${file.label}:${st.line}`;
      const found = secretsIn(hits, st.line, st.endLine, st.text);
      for (let l = st.line; l <= st.endLine; l++) used.add(l);
      if (found.length > 0) {
        upload.skipped.push({ ref, reason: "secret", detail: found[0] });
        scanned.skipped++;
        return;
      }
      let trust: V2.Trust = file.trust;
      if (!onBase(lines, st.raw)) {
        trust = "external";
        scanned.branchOnly++;
      }
      const locator: Record<string, unknown> = {
        path: file.label,
        line: st.line,
        file_kind: file.kind,
      };
      if (st.heading.length > 0) locator.heading = st.heading.join(" › ");
      if (file.location === "repository" && o.branch?.commit)
        locator.commit = o.branch.commit;
      const sources: V2.SourceInput[] = [
        { kind: "file", ref, uri: file.label, locator, trust },
      ];
      // An agent's note that names a web page may be repeating it: the page
      // is an outside source, and the note is quarantined with it.
      if (file.trust === "agent_own_work") {
        for (const url of [...new Set(st.text.match(URL_IN_TEXT) ?? [])].slice(
          0,
          3,
        )) {
          let host = url;
          try {
            host = new URL(url).hostname;
          } catch {
            // not a URL after all; cite it as written
          }
          sources.push({
            kind: "url",
            ref: host.slice(0, 500),
            uri: url.slice(0, 2048),
          });
        }
      }
      const item: V2.ImportItemInput = {
        key: `f${fi}l${st.line}s${si}`,
        ref,
        location: file.location,
        statement: st.text,
        section: st.section,
        kind: st.kind,
        sources,
      };
      const hidden = totalHidden(st.hidden);
      if (hidden > 0) item.hidden_characters = hidden;
      if (split.paths.length > 0) item.scope = { paths: split.paths };
      if (st.kind === "decision" && st.heading.length > 0)
        item.decision = {
          area: st.heading[st.heading.length - 1].slice(0, 500),
        };
      upload.items.push(item);
      scanned.sent++;
    });
    for (const line of split.tooLong) {
      upload.skipped.push({ ref: `${file.label}:${line}`, reason: "too_long" });
      scanned.skipped++;
    }
    if (split.overLimit.length > 0) {
      upload.skipped.push({
        ref: `${file.label}:${split.overLimit[0]}`,
        reason: "limit",
        detail: `${split.overLimit.length} more statements`,
      });
      scanned.skipped += split.overLimit.length;
    }
    scanned.otherSecrets = new Set(
      hits.filter((h) => !used.has(h.line)).map((h) => h.line),
    ).size;
    upload.files.push({
      path: file.label,
      kind: file.kind,
      agent: file.agent,
      location: file.location,
      trust: file.trust,
      statements: scanned.statements,
      skipped: scanned.skipped,
      hidden_characters: totalHidden(split.hidden),
    });
  }
  return out;
}
