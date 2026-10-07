import { AgentStamp } from "../provenance/agent-stamp";
import { Cite } from "../provenance/cite";
import { Highlight } from "../provenance/highlight";
import { Receipt } from "../provenance/receipt";
import { PreviewFrame, type PreviewProps } from "./frame";

export function AgentStampPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="agent-stamp" {...props}>
      <div className="mx-stage mx-stack">
        <div className="mx-inline pv-gap">
          <AgentStamp agent="claude-code" showName surface />
          <AgentStamp agent="codex" showName surface />
          <AgentStamp agent="cursor" showName surface />
          <AgentStamp agent="chatgpt" showName surface />
        </div>
        <div className="mx-inline pv-gap">
          <AgentStamp person="ZZ" name="You" showName />
          <AgentStamp person="JY" name="Jiahao" showName />
          <AgentStamp agent="dream" showName />
          <span className="mx-inline">
            <AgentStamp agent="gemini" size="sm" />
            <AgentStamp agent="opencode" size="sm" />
            <AgentStamp agent="copilot" size="sm" />
            <AgentStamp agent="claude" size="sm" />
          </span>
        </div>
      </div>
    </PreviewFrame>
  );
}

export function ReceiptPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="receipt" {...props}>
      <div className="mx-stage mx-stack">
        <Receipt
          agent="codex"
          action="proposed"
          time="14:02"
          source="session 8f2c"
          id="M-0431"
        />
        <Receipt
          person="ZZ"
          name="You"
          action="kept"
          time="Oct 2, 10:58"
          id="M-0219"
        />
        <Receipt
          agent="dream"
          action="merged 9 notes"
          time="03:12"
          id="M-0219"
        />
        <Receipt
          agent="claude-code"
          action="read"
          time="2 min ago"
          source="CLAUDE.md"
        />
      </div>
    </PreviewFrame>
  );
}

export function CitePreview(props: PreviewProps) {
  return (
    <PreviewFrame name="cite" {...props}>
      <div className="mx-stage">
        <p className="mx-answer pv-measure">
          River runs on the Postgres we already operate, so a job commits in the
          same transaction as the rows it touches.
          <Cite n={1} title="M-0219" href="#M-0219" /> Temporal was tried in
          August and dropped.
          <Cite n={2} title="M-0144" href="#M-0144" />
        </p>
      </div>
    </PreviewFrame>
  );
}

export function HighlightPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="highlight" {...props}>
      <div className="mx-stage">
        <p className="mx-answer pv-measure">
          <Highlight>River runs on the Postgres we already operate</Highlight>,
          so a job commits in the same transaction as the rows it touches.
        </p>
      </div>
    </PreviewFrame>
  );
}
