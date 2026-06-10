"use client";

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocale } from "@/i18n";
import { useAdminAudiences } from "@/hooks/use-admin-audiences";
import type {
  AdminAudienceRule,
  AdminAudienceRuleType,
  AdminAudienceSnapshot,
} from "@/lib/admin-client";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  Skeleton,
} from "@memaxlabs/ui";

const inputClass =
  "w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10";

export interface AudiencePickerValue {
  audienceID?: string;
  snapshot?: AdminAudienceSnapshot;
}

/**
 * Handle exposed via `ref` so the hosting form can force the in-progress
 * textarea draft to be parsed before submit. Returns the flushed value
 * (the parent can't rely on `value` immediately because React state
 * updates batch; `onChange` has fired but the next render hasn't).
 *
 * **Call only from event handlers / effects — never during render.**
 * `flush()` may invoke the parent's `onChange` synchronously, which
 * schedules a state update. Calling it during the render phase would
 * produce a "Cannot update a component while rendering" warning and,
 * under Strict Mode, a double-commit. It is safe (and intended) to
 * call from form `onSubmit`, button `onClick`, or `useEffect`.
 */
export interface AudiencePickerHandle {
  flush: () => AudiencePickerValue;
}

interface AudiencePickerProps {
  value: AudiencePickerValue;
  onChange: (next: AudiencePickerValue) => void;
}

export const AudiencePicker = forwardRef<
  AudiencePickerHandle,
  AudiencePickerProps
>(function AudiencePicker({ value, onChange }, ref) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.audiencePicker;
  const { data, isLoading } = useAdminAudiences();
  const audiences = data?.audiences ?? [];

  const mode: "saved" | "inline" = value.audienceID ? "saved" : "inline";
  const ruleType: AdminAudienceRuleType = value.snapshot?.rule_type ?? "users";
  const rule: AdminAudienceRule = useMemo(
    () => value.snapshot?.rule ?? {},
    [value.snapshot?.rule],
  );

  function setMode(next: "saved" | "inline") {
    if (next === "saved") {
      onChange({ audienceID: audiences[0]?.id });
    } else {
      onChange({
        snapshot: { rule_type: "users", rule: {} },
      });
    }
  }

  function setRuleType(next: AdminAudienceRuleType) {
    onChange({
      snapshot: { rule_type: next, rule: {} },
    });
  }

  function setRule(patch: Partial<AdminAudienceRule>) {
    onChange({
      snapshot: {
        rule_type: ruleType,
        rule: { ...rule, ...patch },
      },
    });
  }

  // Raw textarea state. We intentionally decouple the visible text from
  // the parsed (user_ids, emails) arrays so users can type commas and
  // whitespace mid-address without having them eaten on every keystroke.
  // Parse happens on blur (and when the picker snapshot is reset
  // externally, e.g. switching rule types or loading a saved campaign).
  const parsedKey = [...(rule.user_ids ?? []), ...(rule.emails ?? [])].join(
    "\n",
  );
  const [userLinesDraft, setUserLinesDraft] = useState(parsedKey);
  const lastParsedKey = useRef(parsedKey);
  useEffect(() => {
    // External snapshot changes (rule-type swap, audience swap, form reset)
    // should win over local draft. We detect them by comparing the joined
    // key; if it differs from what our last commit produced, accept the
    // new upstream value as the visible text.
    if (parsedKey !== lastParsedKey.current) {
      setUserLinesDraft(parsedKey);
      lastParsedKey.current = parsedKey;
    }
  }, [parsedKey]);

  // parseUserLines is the pure parse — kept separate from state effects
  // so flush() can compute the latest snapshot synchronously without
  // waiting for React to flush pending updates.
  function parseUserLines(raw: string): {
    user_ids: string[];
    emails: string[];
  } {
    const lines = raw
      .split(/[,\n]/)
      .map((s) => s.trim())
      .filter(Boolean);
    return {
      user_ids: lines.filter((l) => !l.includes("@")),
      emails: lines.filter((l) => l.includes("@")),
    };
  }

  function commitUserLines(raw: string) {
    const parsed = parseUserLines(raw);
    const nextKey = [...parsed.user_ids, ...parsed.emails].join("\n");
    lastParsedKey.current = nextKey;
    setRule(parsed);
  }

  // flush() is the escape hatch for the hosting form's submit handler:
  // mobile browsers don't always fire `blur` before a form submit, so
  // typing "a@x.com" and tapping Save would silently lose the recipient.
  // The form captures a ref to this and calls flush() before validating.
  useImperativeHandle(
    ref,
    (): AudiencePickerHandle => ({
      flush: () => {
        if (ruleType !== "users") {
          return value;
        }
        const parsed = parseUserLines(userLinesDraft);
        const snapshot: AdminAudienceSnapshot = {
          rule_type: "users",
          rule: { ...rule, user_ids: parsed.user_ids, emails: parsed.emails },
        };
        const next: AudiencePickerValue = { snapshot };
        // Fire onChange so the parent's local state also catches up for
        // the re-render after submit completes.
        const nextKey = [...parsed.user_ids, ...parsed.emails].join("\n");
        if (nextKey !== lastParsedKey.current) {
          lastParsedKey.current = nextKey;
          onChange(next);
        }
        return next;
      },
    }),
    [ruleType, userLinesDraft, rule, value, onChange],
  );

  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <ModeButton
          active={mode === "inline"}
          label={copy.modes.inline}
          onClick={() => setMode("inline")}
        />
        <ModeButton
          active={mode === "saved"}
          label={copy.modes.saved}
          onClick={() => setMode("saved")}
          disabled={audiences.length === 0 && !isLoading}
        />
      </div>

      {mode === "saved" ? (
        isLoading ? (
          <Skeleton className="h-10 w-full" />
        ) : audiences.length === 0 ? (
          <p className="text-[12.5px] text-fg-3">{copy.savedEmpty}</p>
        ) : (
          <Select
            value={value.audienceID ?? ""}
            onValueChange={(v: string) => onChange({ audienceID: v })}
            items={Object.fromEntries(
              audiences.map((a) => [a.id, `${a.name} · ${a.rule_type}`]),
            )}
          >
            <SelectTrigger />
            <SelectContent>
              {audiences.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.name} · {a.rule_type}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      ) : (
        <div className="space-y-3">
          <div className="grid gap-2 sm:grid-cols-3">
            {(["all", "users", "hub"] as AdminAudienceRuleType[]).map((t) => (
              <RuleTypeButton
                key={t}
                active={ruleType === t}
                label={copy.ruleTypes[t]}
                onClick={() => setRuleType(t)}
              />
            ))}
          </div>

          {ruleType === "users" ? (
            <div className="space-y-1.5">
              <p className="text-[12px] text-fg-2">{copy.usersHint}</p>
              <textarea
                rows={4}
                value={userLinesDraft}
                onChange={(e) => setUserLinesDraft(e.target.value)}
                onBlur={(e) => commitUserLines(e.target.value)}
                placeholder={copy.usersPlaceholder}
                className={inputClass}
              />
            </div>
          ) : null}

          {ruleType === "hub" ? (
            <div className="grid gap-2 sm:grid-cols-[1fr_12rem]">
              <input
                value={rule.hub_id ?? ""}
                onChange={(e) => setRule({ hub_id: e.target.value })}
                placeholder={copy.hubIdPlaceholder}
                className={inputClass}
              />
              <input
                value={rule.hub_member_role ?? ""}
                onChange={(e) => setRule({ hub_member_role: e.target.value })}
                placeholder={copy.hubRolePlaceholder}
                className={inputClass}
              />
            </div>
          ) : null}

          {ruleType === "all" ? (
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/8 px-3 py-2 text-[12.5px] text-amber-700 dark:text-amber-300">
              {copy.allWarning}
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
});

function ModeButton({
  active,
  label,
  onClick,
  disabled,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={`rounded-lg px-3 py-1.5 text-[13px] transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 ${
        active
          ? "bg-foreground text-background font-medium"
          : "bg-surface-1 text-fg-2 hover:bg-surface-2"
      }`}
    >
      {label}
    </button>
  );
}

function RuleTypeButton({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-surface border px-3 py-2 text-left transition-colors cursor-pointer ${
        active
          ? "border-fg-1/30 bg-surface-2 text-fg-1"
          : "border-border/50 bg-surface-1 text-fg-2 hover:border-border hover:bg-surface-2"
      }`}
    >
      <span className="text-[13px] font-medium">{label}</span>
    </button>
  );
}
