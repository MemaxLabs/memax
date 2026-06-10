"use client";

import { useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Check } from "lucide-react";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
} from "@memaxlabs/ui";
import { ApiError, apiPost } from "@/lib/api";
import { useLocale } from "@/i18n";

const USE_CASES = [
  "personal_memory",
  "team_knowledge",
  "developer_tooling",
  "other",
] as const;

const AI_TOOLS = [
  "claude",
  "chatgpt",
  "gemini",
  "cursor",
  "claude_code",
  "codex",
  "openclaw",
  "other",
] as const;

const ROLES = [
  "developer",
  "founder",
  "product_design",
  "researcher",
  "other",
] as const;

type Step = "collapsed" | "expanded" | "success";

/**
 * Hero waitlist CTA — two-CTA idle → waitlist form → success.
 * Idle shows a filled primary (waitlist) and a ghost secondary (sign in) as
 * peers so returning users never have to hunt in top-nav chrome. Clicking
 * primary morphs to the full waitlist form; the form's Back control returns
 * to idle with entered data preserved. Posts to the same /v1/waitlist
 * endpoint as /waitlist. Expanded + success still carry a whisper-link to
 * sign-in as a secondary fallback for users who drift into the form.
 */
// Shared focus ring for every interactive control in this component. Matches
// pill.tsx / badge.tsx convention across the design system.
const FOCUS_RING =
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

const ERROR_ID_EXPANDED = "hero-waitlist-error-expanded";
const EXPANDED_TITLE_ID = "hero-waitlist-expanded-title";

export function HeroWaitlist() {
  const { t } = useLocale();
  const [step, setStep] = useState<Step>("collapsed");
  const [email, setEmail] = useState("");
  const [useCase, setUseCase] = useState("");
  const [aiTools, setAiTools] = useState<string[]>([]);
  const [otherAiTool, setOtherAiTool] = useState("");
  const [role, setRole] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const expandedBodyRef = useRef<HTMLDivElement>(null);
  const primaryButtonRef = useRef<HTMLButtonElement>(null);

  const emailValid = email.trim().includes("@") && email.trim().length >= 3;

  const clearError = () => {
    if (error) setError("");
  };

  const toggleTool = (tool: string) => {
    clearError();
    setAiTools((prev) =>
      prev.includes(tool) ? prev.filter((x) => x !== tool) : [...prev, tool],
    );
  };

  const handleStartWaitlist = () => {
    setError("");
    setStep("expanded");
    // Focus the email input after morph — first field in the form and the
    // one the user actually needs to fill next.
    requestAnimationFrame(() => {
      expandedBodyRef.current
        ?.querySelector<HTMLElement>("[data-first-focus]")
        ?.focus();
    });
  };

  const handleBack = () => {
    setStep("collapsed");
    setError("");
    // Return focus to the primary CTA so keyboard users don't land on <body>
    // after the expanded form unmounts. Form state (email, etc.) is preserved
    // in component state so re-expanding restores everything.
    requestAnimationFrame(() => {
      primaryButtonRef.current?.focus();
    });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    // Re-entrancy guard. The button's `disabled` only flips after React
    // commits, so a fast double-Enter can still fire two POSTs without this.
    if (submitting) return;
    if (!emailValid || !useCase) return;
    setSubmitting(true);
    setError("");
    try {
      const aiToolsPayload = aiTools
        .map((tool) => (tool === "other" ? otherAiTool.trim() : tool))
        .filter((tool) => tool.length > 0);
      await apiPost("/v1/waitlist", {
        email: email.trim(),
        use_case: useCase,
        ai_tools: aiToolsPayload,
        role,
      });
      setStep("success");
    } catch (err) {
      // Map server codes → localized copy. Never surface raw ApiError.message
      // which is English-only from the Go handler.
      if (err instanceof ApiError && err.code === "invalid_email") {
        setError(t.landing.ctaErrorInvalidEmail);
      } else {
        setError(t.landing.ctaErrorGeneric);
      }
    } finally {
      setSubmitting(false);
    }
  };

  if (step === "success") {
    return (
      <div className="w-full max-w-md mx-auto">
        <div
          className="rounded-surface border border-border/50 px-6 py-7 text-center animate-fade-up"
          style={{
            background: "oklch(from var(--card) l c h / 0.92)",
            backdropFilter: "blur(24px) saturate(140%)",
            WebkitBackdropFilter: "blur(24px) saturate(140%)",
            boxShadow:
              "0 25px 50px -12px oklch(from var(--foreground) l c h / 0.08)",
          }}
          role="status"
          aria-live="polite"
        >
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-surface bg-surface-1">
            <Check className="h-5 w-5 text-fg-2" />
          </div>
          <h2 className="text-[22px] font-semibold tracking-tight text-fg-1">
            {t.landing.ctaSuccessHeadline}
          </h2>
          <p className="mt-3 text-[15px] leading-relaxed text-fg-3 max-w-xs mx-auto">
            {t.landing.ctaSuccessMessage}
          </p>
          <p className="mt-5 text-[13px] text-fg-4">
            {t.landing.ctaSignInPrompt}{" "}
            <a
              href="/login"
              className={`text-fg-2 underline underline-offset-2 hover:text-fg-1 transition-colors rounded-sm ${FOCUS_RING}`}
            >
              {t.landing.ctaSignIn} {"\u2192"}
            </a>
          </p>
        </div>
      </div>
    );
  }

  if (step === "expanded") {
    const emailInvalid = !!error && !emailValid;
    return (
      <div className="w-full max-w-md mx-auto">
        <form
          onSubmit={handleSubmit}
          aria-labelledby={EXPANDED_TITLE_ID}
          className="animate-fade-up rounded-surface border border-border/50 overflow-hidden"
          style={{
            background: "oklch(from var(--card) l c h / 0.92)",
            backdropFilter: "blur(24px) saturate(140%)",
            WebkitBackdropFilter: "blur(24px) saturate(140%)",
            boxShadow:
              "0 25px 50px -12px oklch(from var(--foreground) l c h / 0.08)",
          }}
        >
          {/* Visually-hidden region label — gives screen readers structural
              context when the morph swaps the pill for this form. */}
          <h2 id={EXPANDED_TITLE_ID} className="sr-only">
            {t.landing.ctaExpandHeadline}
          </h2>

          {/* Back bar — returns to the collapsed pill with the typed email
              preserved. Tiny, muted, sits inside the form container so it
              feels like part of the morph, not a separate chrome element. */}
          <div className="px-5 pt-3 pb-3">
            <button
              type="button"
              onClick={handleBack}
              className={`inline-flex items-center gap-1 text-[12px] text-fg-4 hover:text-fg-2 transition-colors cursor-pointer rounded-sm ${FOCUS_RING}`}
            >
              <ArrowLeft className="w-3 h-3" strokeWidth={2} />
              {t.landing.ctaBack}
            </button>
          </div>

          <div ref={expandedBodyRef} className="px-5 pb-5 space-y-5">
            {/* Email — carried over, still editable. Focused after morph
                so a typo is one keystroke away from being corrected. */}
            <div>
              <label
                htmlFor="hero-waitlist-email"
                className="block text-[12px] font-medium text-fg-3 mb-1.5"
              >
                {t.waitlistPage.emailLabel}
              </label>
              <input
                id="hero-waitlist-email"
                data-first-focus
                type="email"
                required
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  clearError();
                }}
                aria-invalid={emailInvalid || undefined}
                aria-describedby={error ? ERROR_ID_EXPANDED : undefined}
                className="w-full rounded-chrome border border-border/50 bg-background px-4 py-3 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
              />
            </div>

            {/* Use case */}
            <div>
              <label
                id="hero-waitlist-usecase-label"
                className="block text-[12px] font-medium text-fg-3 mb-1.5"
              >
                {t.waitlistPage.useCaseLabel}
              </label>
              <Select
                value={useCase}
                onValueChange={(v: string) => {
                  setUseCase(v);
                  clearError();
                }}
                required
                items={Object.fromEntries(
                  USE_CASES.map((uc) => [
                    uc,
                    t.waitlistPage.useCaseOptions[uc],
                  ]),
                )}
              >
                <SelectTrigger
                  aria-labelledby="hero-waitlist-usecase-label"
                  className="rounded-chrome bg-background py-3"
                />
                <SelectContent>
                  {USE_CASES.map((uc) => (
                    <SelectItem key={uc} value={uc}>
                      {t.waitlistPage.useCaseOptions[uc]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* AI tools — chip group is a named region so screen readers
                announce the label once, not per chip. */}
            <div>
              <span
                id="hero-waitlist-aitools-label"
                className="block text-[12px] font-medium text-fg-3 mb-2"
              >
                {t.waitlistPage.aiToolsLabel}
              </span>
              <div
                role="group"
                aria-labelledby="hero-waitlist-aitools-label"
                className="flex flex-wrap gap-2"
              >
                {AI_TOOLS.map((tool) => {
                  const selected = aiTools.includes(tool);
                  return (
                    <button
                      key={tool}
                      type="button"
                      onClick={() => toggleTool(tool)}
                      aria-pressed={selected}
                      className={`px-3 py-1.5 rounded-chrome text-[13px] border transition-colors cursor-pointer ${FOCUS_RING} ${
                        selected
                          ? "bg-foreground text-background border-foreground"
                          : "bg-background text-fg-2 border-border/50 hover:border-fg-3"
                      }`}
                    >
                      {t.waitlistPage.aiToolOptions[tool]}
                    </button>
                  );
                })}
              </div>
              {aiTools.includes("other") && (
                <input
                  type="text"
                  value={otherAiTool}
                  onChange={(e) => setOtherAiTool(e.target.value)}
                  placeholder={t.waitlistPage.aiToolsOtherPlaceholder}
                  maxLength={60}
                  aria-label={t.waitlistPage.aiToolsOtherPlaceholder}
                  className={`mt-2 w-full rounded-chrome border border-border/50 bg-background px-3 py-2 text-[13px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors ${FOCUS_RING}`}
                />
              )}
            </div>

            {/* Role */}
            <div>
              <label
                id="hero-waitlist-role-label"
                className="block text-[12px] font-medium text-fg-3 mb-1.5"
              >
                {t.waitlistPage.roleLabel}
              </label>
              <Select
                value={role}
                onValueChange={(v: string) => {
                  setRole(v);
                  clearError();
                }}
                items={Object.fromEntries(
                  ROLES.map((r) => [r, t.waitlistPage.roleOptions[r]]),
                )}
              >
                <SelectTrigger
                  aria-labelledby="hero-waitlist-role-label"
                  className="rounded-chrome bg-background py-3"
                />
                <SelectContent>
                  {ROLES.map((r) => (
                    <SelectItem key={r} value={r}>
                      {t.waitlistPage.roleOptions[r]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="px-5 py-4 border-t border-border/30">
            {error && (
              <p
                id={ERROR_ID_EXPANDED}
                className="mb-3 text-[13px] text-red-500"
                role="alert"
              >
                {error}
              </p>
            )}
            <button
              type="submit"
              disabled={submitting || !emailValid || !useCase}
              className={`h-11 w-full rounded-[20px] bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center cursor-pointer hover:opacity-90 active:opacity-80 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed ${FOCUS_RING}`}
            >
              {submitting ? t.landing.ctaJoining : t.landing.ctaSubmit}
            </button>
            {/* Sign-in escape — mirrors the collapsed pill's link so users
                who drift into the form and realise they have access can pivot
                without hunting in the top nav. */}
            <p className="mt-3 text-center text-[12px] text-fg-4">
              {t.landing.ctaSignInPrompt}{" "}
              <a
                href="/login"
                className={`text-fg-3 hover:text-fg-2 transition-colors underline underline-offset-2 rounded-sm ${FOCUS_RING}`}
              >
                {t.landing.ctaSignIn} {"\u2192"}
              </a>
            </p>
          </div>
        </form>
      </div>
    );
  }

  return (
    <div className="w-full max-w-md mx-auto">
      {/* Two-CTA idle. Same chrome at all breakpoints — primary filled,
          secondary ghost with border — so hierarchy is carried by fill/color
          instead of asymmetric treatment between breakpoints. Stacks on
          mobile, side-by-side on desktop. Secondary remains a real <a> so
          keyboard activation, middle-click, and Cmd-click all work. */}
      <div className="flex flex-col sm:flex-row gap-2 w-full">
        <button
          ref={primaryButtonRef}
          type="button"
          onClick={handleStartWaitlist}
          className={`sm:flex-1 inline-flex items-center justify-center gap-1.5 h-12 px-5 rounded-[20px] text-[14px] font-semibold bg-foreground text-background hover:opacity-90 active:opacity-80 transition-opacity cursor-pointer ${FOCUS_RING}`}
        >
          {t.landing.ctaJoin}
          <ArrowRight className="w-3.5 h-3.5" strokeWidth={2.2} />
        </button>
        <a
          href="/login"
          className={`inline-flex items-center justify-center h-12 px-6 rounded-[20px] text-[14px] font-medium bg-transparent border border-border/60 text-fg-2 hover:text-fg-1 hover:bg-surface-1 transition-colors cursor-pointer ${FOCUS_RING}`}
        >
          {t.landing.ctaSignIn}
        </a>
      </div>
    </div>
  );
}
