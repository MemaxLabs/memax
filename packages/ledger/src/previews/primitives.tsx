import { Button } from "../primitives/button";
import { Field } from "../primitives/field";
import { Kbd } from "../primitives/kbd";
import { Segmented } from "../primitives/segmented";
import { PreviewFrame, type PreviewProps } from "./frame";

export function ButtonPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="button" {...props}>
      <div className="mx-stage mx-stack">
        <div className="mx-inline">
          <Button variant="primary" kbd="R">
            Start review
          </Button>
          <Button variant="keep" kbd="K">
            Keep
          </Button>
          <Button variant="secondary" icon="handoff">
            Hand off
          </Button>
          <Button variant="quiet" kbd="X">
            Reject
          </Button>
          <Button variant="danger" icon="forget">
            Forget
          </Button>
          <Button variant="secondary" icon="sync" aria-label="Recompile" />
        </div>
        <div className="mx-inline">
          <Button variant="primary" size="lg">
            Review 33 imports
          </Button>
          <Button variant="secondary" size="sm">
            Copy as prompt
          </Button>
          <Button
            variant="keep"
            size="sm"
            disabled
            disabledReason="Choose 1, 2 or 3"
          >
            Keep
          </Button>
        </div>
      </div>
    </PreviewFrame>
  );
}

export function KbdPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="kbd" {...props}>
      <div className="mx-stage mx-inline pv-keys">
        <span>
          <Kbd>⌘K</Kbd> ask
        </span>
        <span>
          <Kbd>K</Kbd> keep
        </span>
        <span>
          <Kbd>E</Kbd> edit
        </span>
        <span>
          <Kbd>X</Kbd> reject
        </span>
        <span>
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd> move
        </span>
        <span>
          <Kbd>⌘↵</Kbd> keep answer
        </span>
      </div>
    </PreviewFrame>
  );
}

export function FieldPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="field" {...props}>
      <div className="mx-stage pv-fields">
        <Field
          label="Space name"
          defaultValue="memax-v2"
          hint="Agents see this name in receipts."
        />
        <Field
          label="Compile target"
          mono
          icon="file"
          defaultValue=".cursor/rules/memax.mdc"
        />
        <Field
          label="Stale after"
          defaultValue="90 days"
          error="Must be between 7 and 365 days."
        />
      </div>
    </PreviewFrame>
  );
}

export function SegmentedPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="segmented" {...props}>
      <div className="mx-stage mx-inline pv-gap">
        <Segmented
          label="Autonomy"
          defaultValue="propose"
          options={[
            { value: "read", label: "Read" },
            { value: "propose", label: "Propose" },
            { value: "write", label: "Write" },
          ]}
        />
        <Segmented
          size="sm"
          label="Filter"
          defaultValue="all"
          options={[
            { value: "all", label: "All 4" },
            { value: "c", label: "Conflicts 1" },
            { value: "e", label: "External 1" },
          ]}
        />
      </div>
    </PreviewFrame>
  );
}
