import { useState } from "react";
import { Button } from "../primitives/button";
import { Diff } from "../memory/diff";
import { DreamCard } from "../memory/dream-card";
import { Lineage } from "../memory/lineage";
import { MemoryList } from "../memory/memory-list";
import { MemoryRow } from "../memory/memory-row";
import { Redaction } from "../memory/redaction";
import { ReviewCard } from "../memory/review-card";
import { StateMark } from "../memory/state-mark";
import type { MarkState } from "../lib/types";
import { PreviewFrame, type PreviewProps } from "./frame";

const MARK_STATES: MarkState[] = [
  "proposed",
  "kept",
  "merged",
  "stale",
  "faded",
  "conflict",
  "forgotten",
  "working",
  "off",
];

export function StateMarkPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="state-mark" {...props}>
      <div className="mx-stage mx-inline pv-gap">
        {MARK_STATES.map((state) => (
          <StateMark key={state} state={state} />
        ))}
      </div>
    </PreviewFrame>
  );
}

export function MemoryRowPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="memory-row" {...props}>
      <div className="mx-stage">
        <div className="mx-panel">
          <MemoryList>
            <MemoryRow
              person="ZZ"
              name="You"
              action="kept"
              time="Oct 2"
              space="memax-v2"
              id="M-0219"
              actions={
                <>
                  <Button
                    size="sm"
                    variant="quiet"
                    icon="pencil"
                    aria-label="Edit"
                  />
                  <Button
                    size="sm"
                    variant="quiet"
                    icon="forget"
                    aria-label="Forget"
                  />
                </>
              }
            >
              Background jobs run on River, not Temporal.
            </MemoryRow>
            <MemoryRow
              selected
              state="proposed"
              agent="codex"
              action="proposed"
              time="22 min ago"
              space="memax-v2"
              id="M-0432"
            >
              Pin shared dependency versions with pnpm catalog:.
            </MemoryRow>
            <MemoryRow
              state="stale"
              agent="dream"
              action="flagged"
              time="03:12"
              space="memax-v2"
              id="M-0187"
              note="Its source changed on Sep 11 · PR #198"
            >
              Ask memax answers with the Haiku tier.
            </MemoryRow>
            <MemoryRow
              state="conflict"
              person="JY"
              name="Jiahao"
              action="kept"
              time="Sep 18"
              space="memax-v2"
              id="M-0174"
              note="Conflicts with M-0431, proposed by Codex"
            >
              Deploy the v2 API to Railway.
            </MemoryRow>
            <MemoryRow
              state="merged"
              agent="cursor"
              action="merged"
              time="Oct 5"
              id="N-1203 → M-0219"
            >
              Jobs live in packages/server/internal/queue.
            </MemoryRow>
          </MemoryList>
        </div>
      </div>
    </PreviewFrame>
  );
}

export function RedactionPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="redaction" {...props}>
      <div className="mx-stage">
        <div className="mx-panel">
          <MemoryList>
            <Redaction
              date="Oct 3"
              id="M-0388"
              detail="removed from 4 files and 5 agents"
            />
            <Redaction date="Sep 29" by="Jiahao" id="M-0301" width="44%" />
          </MemoryList>
        </div>
      </div>
    </PreviewFrame>
  );
}

export function DiffPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="diff" {...props}>
      <div className="mx-stage">
        <Diff
          className="pv-big"
          before="MCP write tools must ask for confirmation through elicitation."
          after="MCP write tools must ask for confirmation with input_required."
        />
      </div>
    </PreviewFrame>
  );
}

export function LineagePreview(props: PreviewProps) {
  return (
    <PreviewFrame name="lineage" {...props}>
      <div className="mx-stage">
        <div className="pv-measure">
          <Lineage
            events={[
              {
                key: "proposed",
                state: "proposed",
                agent: "claude-code",
                title: "Proposed by Claude Code",
                time: "Oct 2, 10:41",
                detail: "While moving the dream workers in session 3e1a.",
              },
              {
                key: "kept",
                state: "kept",
                person: "ZZ",
                title: "Kept by you",
                time: "Oct 2, 10:58",
                detail: "Edited “prefer River” to “River, not Temporal”.",
              },
              {
                key: "merged",
                state: "merged",
                agent: "dream",
                title: "Dream merged 9 notes into it",
                time: "Oct 5, 03:12",
              },
              {
                key: "handoff",
                agent: "codex",
                title: "Handed to Codex in H-0093",
                time: "Oct 5, 14:20",
              },
              {
                key: "verified",
                title: "Checked against the code",
                time: "Oct 5, 14:31",
                detail: "PR #212 is merged. Still true.",
              },
            ]}
          />
        </div>
      </div>
    </PreviewFrame>
  );
}

/** Interactive: Keep stamps the seal on the first card; Undo returns the second to proposed. */
export function ReviewCardPreview(props: PreviewProps) {
  const [firstKept, setFirstKept] = useState(false);
  const [secondKept, setSecondKept] = useState(true);
  return (
    <PreviewFrame name="review-card" {...props}>
      <div className="mx-stage">
        <div className="pv-col">
          <ReviewCard
            agent="claude-code"
            time="1 h ago"
            space="memax-v2"
            source="session 3e1a"
            id="M-0430"
            before="MCP write tools must ask for confirmation through elicitation."
            beforeId="M-0156"
            statement="MCP write tools must ask for confirmation with input_required."
            external="Claude Code read this in the MCP specification. Check the claim against the source before keeping it."
            evidence="“A server that needs input returns an input_required result, and the client retries with the answer.”"
            evidenceSource="modelcontextprotocol.io · spec 2026-07-28"
            kept={firstKept}
            keptBy="ZZ"
            keptDate="Oct 5"
            onKeep={() => setFirstKept(true)}
            onUndo={() => setFirstKept(false)}
          />
          <ReviewCard
            agent="codex"
            time="22 min ago"
            space="memax-v2"
            id="M-0432"
            statement="Pin shared dependency versions with pnpm catalog:."
            kept={secondKept}
            keptBy="ZZ"
            keptDate="Oct 5"
            onKeep={() => setSecondKept(true)}
            onUndo={() => setSecondKept(false)}
          />
        </div>
      </div>
    </PreviewFrame>
  );
}

export function DreamCardPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="dream-card" {...props}>
      <div className="mx-stage">
        <div className="pv-measure">
          <DreamCard
            issue={214}
            date="Mon 5 Oct"
            time="03:12"
            duration="41s"
            notes={34}
            facts={6}
            noteIds={Array.from({ length: 34 }, (_, i) => `N-${1180 + i}`)}
            factIds={[
              "M-0219",
              "M-0434",
              "M-0435",
              "M-0436",
              "M-0437",
              "M-0438",
            ]}
            items={[
              {
                key: "merged",
                kind: "merged",
                text: "Background jobs run on River, not Temporal.",
                meta: "9 notes → M-0219",
              },
              {
                key: "conflict",
                kind: "conflict",
                text: "Deploy target: Fly.io (Codex) or Railway (Jiahao)?",
                meta: "needs you",
              },
              {
                key: "faded",
                kind: "faded",
                text: "11 notes unread by any agent for 60 days.",
                meta: "restorable",
              },
            ]}
            action={
              <>
                <Button variant="primary" size="sm" kbd="C">
                  Resolve conflict
                </Button>
                <Button variant="secondary" size="sm">
                  Read the edition
                </Button>
              </>
            }
          />
        </div>
      </div>
    </PreviewFrame>
  );
}
