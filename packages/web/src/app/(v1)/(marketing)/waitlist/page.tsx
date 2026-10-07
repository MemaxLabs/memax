"use client";

import { useState } from "react";
import { useLocale } from "@/i18n";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
} from "@memaxlabs/ui";
import { DREAM_AMBIENT } from "@memaxlabs/ui/tokens/dream";
import { apiPost } from "@/lib/api";
import { Check } from "lucide-react";

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

export default function WaitlistPage() {
  const { t } = useLocale();
  const [email, setEmail] = useState("");
  const [useCase, setUseCase] = useState("");
  const [aiTools, setAiTools] = useState<string[]>([]);
  const [otherAiTool, setOtherAiTool] = useState("");
  const [role, setRole] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [error, setError] = useState("");

  const toggleTool = (tool: string) => {
    setAiTools((prev) =>
      prev.includes(tool) ? prev.filter((t) => t !== tool) : [...prev, tool],
    );
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email || !useCase) return;

    setSubmitting(true);
    setError("");
    try {
      const aiToolsPayload = aiTools
        .map((tool) => (tool === "other" ? otherAiTool.trim() : tool))
        .filter((tool) => tool.length > 0);
      await apiPost("/v1/waitlist", {
        email,
        use_case: useCase,
        ai_tools: aiToolsPayload,
        role,
      });
      setSubmitted(true);
    } catch {
      setError(t.states.error.unexpected);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="relative flex min-h-dvh items-center justify-center px-4 py-16 bg-background overflow-hidden">
      {/* Ambient dream glow */}
      <div
        className="pointer-events-none fixed inset-0"
        style={{ background: DREAM_AMBIENT }}
      />

      <div className="relative z-10 w-full max-w-md">
        {/* Wordmark */}
        <div className="flex justify-center mb-10">
          <div
            className="h-7 w-36"
            style={{
              backgroundColor: "var(--foreground)",
              opacity: 0.85,
              maskImage: "url(/images/memax-wordmark.svg)",
              maskSize: "contain",
              maskRepeat: "no-repeat",
              maskPosition: "center",
              WebkitMaskImage: "url(/images/memax-wordmark.svg)",
              WebkitMaskSize: "contain",
              WebkitMaskRepeat: "no-repeat",
              WebkitMaskPosition: "center",
            }}
          />
        </div>

        {submitted ? (
          /* Success state */
          <div className="animate-fade-up text-center">
            <div className="mx-auto mb-5 flex h-14 w-14 items-center justify-center rounded-chrome bg-surface-1">
              <Check className="h-6 w-6 text-fg-2" />
            </div>
            <h1 className="text-[22px] font-semibold tracking-tight text-fg-1">
              {t.waitlistPage.successHeadline}
            </h1>
            <p className="mt-3 text-[15px] leading-relaxed text-fg-3 max-w-xs mx-auto">
              {t.waitlistPage.successMessage}
            </p>
          </div>
        ) : (
          /* Signup form */
          <form onSubmit={handleSubmit} className="animate-fade-up">
            {/* Header */}
            <div className="text-center mb-8">
              <h1 className="text-[22px] font-semibold tracking-tight text-fg-1">
                {t.waitlistPage.headline}
              </h1>
              <p className="mt-2 text-[14px] leading-relaxed text-fg-3 max-w-sm mx-auto">
                {t.waitlistPage.subtitle}
              </p>
            </div>

            {/* Form card */}
            <div
              className="rounded-surface border border-border/50 overflow-hidden"
              style={{
                background: "oklch(from var(--card) l c h / 0.92)",
                backdropFilter: "blur(24px) saturate(140%)",
                WebkitBackdropFilter: "blur(24px) saturate(140%)",
                boxShadow:
                  "0 25px 50px -12px oklch(from var(--foreground) l c h / 0.08)",
              }}
            >
              <div className="px-6 py-6 space-y-5">
                {/* Email */}
                <div>
                  <label className="block text-[12px] font-medium text-fg-3 mb-1.5">
                    {t.waitlistPage.emailLabel}
                  </label>
                  <input
                    type="email"
                    required
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder={t.waitlistPage.emailPlaceholder}
                    className="w-full rounded-chrome border border-border/50 bg-background px-4 py-3 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
                  />
                </div>

                {/* Use case */}
                <div>
                  <label className="block text-[12px] font-medium text-fg-3 mb-1.5">
                    {t.waitlistPage.useCaseLabel}
                  </label>
                  <Select
                    value={useCase}
                    onValueChange={(v: string) => setUseCase(v)}
                    required
                    items={Object.fromEntries(
                      USE_CASES.map((uc) => [
                        uc,
                        t.waitlistPage.useCaseOptions[uc],
                      ]),
                    )}
                  >
                    <SelectTrigger className="rounded-chrome bg-background py-3" />
                    <SelectContent>
                      {USE_CASES.map((uc) => (
                        <SelectItem key={uc} value={uc}>
                          {t.waitlistPage.useCaseOptions[uc]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {/* AI tools — multi-select chips */}
                <div>
                  <label className="block text-[12px] font-medium text-fg-3 mb-2">
                    {t.waitlistPage.aiToolsLabel}
                  </label>
                  <div className="flex flex-wrap gap-2">
                    {AI_TOOLS.map((tool) => {
                      const selected = aiTools.includes(tool);
                      return (
                        <button
                          key={tool}
                          type="button"
                          onClick={() => toggleTool(tool)}
                          className={`px-3 py-1.5 rounded-chrome text-[13px] border transition-colors cursor-pointer ${
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
                      className="mt-2 w-full rounded-chrome border border-border/50 bg-background px-3 py-2 text-[13px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
                    />
                  )}
                </div>

                {/* Role */}
                <div>
                  <label className="block text-[12px] font-medium text-fg-3 mb-1.5">
                    {t.waitlistPage.roleLabel}
                  </label>
                  <Select
                    value={role}
                    onValueChange={(v: string) => setRole(v)}
                    items={Object.fromEntries(
                      ROLES.map((r) => [r, t.waitlistPage.roleOptions[r]]),
                    )}
                  >
                    <SelectTrigger className="rounded-chrome bg-background py-3" />
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

              {/* Submit */}
              <div className="px-6 py-5 border-t border-border/30">
                {error && (
                  <p className="mb-3 text-[13px] text-red-500">{error}</p>
                )}
                <button
                  type="submit"
                  disabled={submitting || !email || !useCase}
                  className="h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center cursor-pointer hover:opacity-90 active:opacity-80 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
                >
                  {submitting
                    ? t.waitlistPage.submitting
                    : t.waitlistPage.submit}
                </button>
              </div>
            </div>

            {/* Legal */}
            <p className="mt-5 text-center text-[11px] leading-relaxed text-fg-4">
              {t.auth.legal}{" "}
              <a
                href="/terms"
                className="text-fg-3 underline underline-offset-2 hover:text-fg-2 transition-colors"
              >
                {t.auth.terms}
              </a>
              {" & "}
              <a
                href="/privacy"
                className="text-fg-3 underline underline-offset-2 hover:text-fg-2 transition-colors"
              >
                {t.auth.privacy}
              </a>
            </p>
          </form>
        )}
      </div>
    </main>
  );
}
