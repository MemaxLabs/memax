// Maps to: /memories/[id] destructive morph
// ui/ components: NoteHeader (normal + forgetting + forgotten states)
"use client";

import { Trash2 } from "lucide-react";
import { Section, DemoCard } from "../_shared";

export function ForgetSection() {
  return (
    <Section
      title="3. Forget Experience"
      description="Container morphs from neutral to glassmorphism destructive tint. No modal — the surface transforms in place. Three states: normal, forgetting, forgotten."
    >
      <DemoCard label="Note header states">
        <div className="space-y-4">
          {/* Normal state */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Normal
            </p>
            <div
              className="flex items-center justify-between px-4 py-3 rounded-xl border border-border/40"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
              }}
            >
              <span className="text-[14px] text-fg-2">Normal note header</span>
              <div className="flex items-center gap-3">
                {/* Text option */}
                <button className="text-[13px] text-fg-3 hover:text-fg-2 transition-colors">
                  Forget
                </button>
                {/* Icon option */}
                <button
                  className="text-fg-4 hover:text-fg-3 transition-colors"
                  title="Forget"
                >
                  <Trash2 className="size-3.5" />
                </button>
              </div>
            </div>
          </div>

          {/* Forgetting state — glassmorphism treatment */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Forgetting
            </p>
            <div
              className="flex items-center justify-between px-4 py-3 rounded-xl"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.08)",
                border: "1px solid oklch(from var(--destructive) l c h / 0.15)",
                backdropFilter: "blur(12px)",
                WebkitBackdropFilter: "blur(12px)",
                boxShadow: "inset 0 1px 0 oklch(1 0 0 / 0.05)",
              }}
            >
              <span className="text-[14px] text-fg-2">Forgetting&hellip;</span>
              <div className="flex items-center gap-2">
                <button
                  className="text-[13px] px-3 py-1 rounded-full transition-colors"
                  style={{
                    background: "oklch(from var(--destructive) l c h / 0.12)",
                    border:
                      "1px solid oklch(from var(--destructive) l c h / 0.2)",
                    color: "oklch(from var(--destructive) l c h / 0.8)",
                  }}
                >
                  Forget
                </button>
                <button
                  className="text-[13px] px-3 py-1 rounded-full font-medium transition-colors"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.05)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.1)",
                    color: "oklch(from var(--foreground) l c h / 0.7)",
                  }}
                >
                  Keep
                </button>
              </div>
            </div>
          </div>

          {/* Forgotten state — dissolved, blurred, faded */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Forgotten
            </p>
            <div
              className="flex items-center justify-between px-4 py-3 rounded-xl"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.03)",
                border: "1px solid oklch(from var(--destructive) l c h / 0.06)",
                backdropFilter: "blur(16px)",
                WebkitBackdropFilter: "blur(16px)",
                boxShadow: "inset 0 1px 0 oklch(1 0 0 / 0.03)",
              }}
            >
              <span
                className="text-[14px] line-through"
                style={{ color: "oklch(from var(--foreground) l c h / 0.3)" }}
              >
                This memory has been forgotten
              </span>
              <span
                className="text-[12px]"
                style={{ color: "oklch(from var(--foreground) l c h / 0.2)" }}
              >
                Undo
              </span>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* Full card demo — showing the morph in a wider context */}
      <DemoCard label="Full card morph preview">
        <div className="space-y-4">
          {/* Normal card */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Card &mdash; Normal
            </p>
            <div
              className="rounded-xl p-4 space-y-2"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
              }}
            >
              <p className="text-[14px] text-fg-2">
                React Server Components render on the server and stream HTML to
                the client.
              </p>
              <div className="flex items-center justify-between">
                <span className="text-[12px] text-fg-3">3 days ago</span>
                <button className="text-[12px] text-fg-3 hover:text-destructive/60 transition-colors">
                  Forget
                </button>
              </div>
            </div>
          </div>

          {/* Forgetting card */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Card &mdash; Forgetting
            </p>
            <div
              className="rounded-xl p-4 space-y-2"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.08)",
                border: "1px solid oklch(from var(--destructive) l c h / 0.15)",
                backdropFilter: "blur(12px)",
                WebkitBackdropFilter: "blur(12px)",
                boxShadow: "inset 0 1px 0 oklch(1 0 0 / 0.05)",
              }}
            >
              <p className="text-[14px] text-fg-2">
                React Server Components render on the server and stream HTML to
                the client.
              </p>
              <div className="flex items-center justify-between">
                <span className="text-[12px] text-fg-3">Are you sure?</span>
                <div className="flex items-center gap-2">
                  <button
                    className="text-[12px] px-2.5 py-0.5 rounded-full transition-colors"
                    style={{
                      background: "oklch(from var(--destructive) l c h / 0.12)",
                      border:
                        "1px solid oklch(from var(--destructive) l c h / 0.2)",
                      color: "oklch(from var(--destructive) l c h / 0.8)",
                    }}
                  >
                    Forget
                  </button>
                  <button
                    className="text-[12px] px-2.5 py-0.5 rounded-full font-medium transition-colors"
                    style={{
                      background: "oklch(from var(--foreground) l c h / 0.05)",
                      border:
                        "1px solid oklch(from var(--foreground) l c h / 0.1)",
                      color: "oklch(from var(--foreground) l c h / 0.7)",
                    }}
                  >
                    Keep
                  </button>
                </div>
              </div>
            </div>
          </div>

          {/* Forgotten card — dissolved */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Card &mdash; Forgotten
            </p>
            <div
              className="rounded-xl p-4"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.02)",
                border: "1px solid oklch(from var(--destructive) l c h / 0.04)",
                backdropFilter: "blur(20px)",
                WebkitBackdropFilter: "blur(20px)",
                opacity: 0.6,
              }}
            >
              <p
                className="text-[14px] line-through"
                style={{ color: "oklch(from var(--foreground) l c h / 0.25)" }}
              >
                React Server Components render on the server and stream HTML to
                the client.
              </p>
              <div className="flex items-center justify-between mt-2">
                <span
                  className="text-[12px]"
                  style={{
                    color: "oklch(from var(--foreground) l c h / 0.2)",
                  }}
                >
                  Memory forgotten
                </span>
                <button
                  className="text-[12px] transition-colors"
                  style={{
                    color: "oklch(from var(--foreground) l c h / 0.25)",
                  }}
                >
                  Undo
                </button>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
