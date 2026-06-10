// Maps to: features/recalling-text.tsx
// Production now uses crossfade with progressive timing (THRESHOLDS: 0, 1.5s, 3s, 5s, 8s).
// Gaps widen to convey "working harder." Loop mode for idle cycling in dropdown.
// ui/ components: RecallingText (compact + loop props)
"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";

// Must match production i18n (packages/web/src/i18n/locales/en.ts)
const MESSAGES = {
  recall: [
    "memax recalling",
    "connecting the dots",
    "surfacing memories",
    "piecing it together",
  ],
  ask: [
    "thinking...",
    "reading your memories...",
    "connecting the dots...",
    "composing an answer...",
    "almost there...",
  ],
  // Remember uses a static notification ("Remembering..."), not cycling text.
  // Shown here for demo purposes only — production uses barNotification type "info".
  remember: ["remembering..."],
};

const THRESHOLDS = [0, 1500, 3000, 5000, 8000];

/* ── Keyframe styles ── */
const loadingKeyframes = `
@keyframes sonar-ring {
  0% { transform: scale(0.5); opacity: 0.6; }
  100% { transform: scale(2.5); opacity: 0; }
}
@keyframes orbit-spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}
@keyframes settle-bounce {
  0% { transform: translateY(-8px); opacity: 0.4; }
  60% { transform: translateY(2px); opacity: 1; }
  80% { transform: translateY(-1px); }
  100% { transform: translateY(0); opacity: 0.9; }
}
@keyframes progress-ring {
  0% { stroke-dashoffset: 100.53; }
  100% { stroke-dashoffset: 0; }
}
@keyframes star-breathe {
  0%, 100% { transform: scale(1); opacity: 0.7; }
  50% { transform: scale(1.15); opacity: 1; }
}
@keyframes star-pulse {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.3); }
}
@keyframes star-glow {
  0%, 100% { filter: drop-shadow(0 0 2px currentColor); transform: scale(1); }
  50% { filter: drop-shadow(0 0 6px currentColor); transform: scale(1.1); }
}
`;

/** Animated star indicator for each mode */
function StarIndicator({
  mode,
  running,
  elapsed,
  totalDuration,
}: {
  mode: "recall" | "ask" | "remember";
  running: boolean;
  elapsed: number;
  totalDuration: number;
}) {
  const progress = Math.min(elapsed / totalDuration, 1);
  const circumference = 2 * Math.PI * 16; // r=16

  if (mode === "recall") {
    // Sonar/radar expanding rings
    return (
      <div
        className="relative flex items-center justify-center"
        style={{ width: 40, height: 40 }}
      >
        {/* Progress ring */}
        <svg
          className="absolute inset-0"
          width={40}
          height={40}
          viewBox="0 0 40 40"
        >
          <circle
            cx={20}
            cy={20}
            r={16}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            className="text-foreground/8"
          />
          {running && (
            <circle
              cx={20}
              cy={20}
              r={16}
              fill="none"
              strokeWidth={1.5}
              strokeLinecap="round"
              style={{
                stroke: SIGNATURE,
                strokeDasharray: circumference,
                strokeDashoffset: circumference * (1 - progress),
                transform: "rotate(-90deg)",
                transformOrigin: "center",
                transition: "stroke-dashoffset 0.3s ease",
              }}
            />
          )}
        </svg>
        {/* Expanding sonar rings */}
        {running && (
          <>
            <div
              className="absolute rounded-full pointer-events-none"
              style={{
                width: 16,
                height: 16,
                border: `1.5px solid ${SIGNATURE}`,
                animation: "sonar-ring 2s ease-out infinite",
              }}
            />
            <div
              className="absolute rounded-full pointer-events-none"
              style={{
                width: 16,
                height: 16,
                border: `1px solid ${SIGNATURE}`,
                animation: "sonar-ring 2s ease-out infinite 0.7s",
              }}
            />
          </>
        )}
        <span
          className="relative text-[16px] leading-none"
          style={{
            color: SIGNATURE,
            animation: running
              ? "star-breathe 2s ease-in-out infinite"
              : "none",
          }}
        >
          ✦
        </span>
      </div>
    );
  }

  if (mode === "ask") {
    // Orbiting dots
    return (
      <div
        className="relative flex items-center justify-center"
        style={{ width: 40, height: 40 }}
      >
        {/* Progress ring */}
        <svg
          className="absolute inset-0"
          width={40}
          height={40}
          viewBox="0 0 40 40"
        >
          <circle
            cx={20}
            cy={20}
            r={16}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            className="text-foreground/8"
          />
          {running && (
            <circle
              cx={20}
              cy={20}
              r={16}
              fill="none"
              strokeWidth={1.5}
              strokeLinecap="round"
              style={{
                stroke: SIGNATURE,
                strokeDasharray: circumference,
                strokeDashoffset: circumference * (1 - progress),
                transform: "rotate(-90deg)",
                transformOrigin: "center",
                transition: "stroke-dashoffset 0.3s ease",
              }}
            />
          )}
        </svg>
        {/* Orbiting dots */}
        {running && (
          <div
            className="absolute"
            style={{
              width: 32,
              height: 32,
              animation: "orbit-spin 3s linear infinite",
            }}
          >
            {[0, 1, 2].map((i) => {
              const angle = (i / 3) * Math.PI * 2;
              const x = 16 + Math.cos(angle) * 13;
              const y = 16 + Math.sin(angle) * 13;
              return (
                <div
                  key={i}
                  className="absolute rounded-full"
                  style={{
                    width: 3,
                    height: 3,
                    backgroundColor: SIGNATURE,
                    left: x - 1.5,
                    top: y - 1.5,
                    opacity: 0.5 + i * 0.15,
                  }}
                />
              );
            })}
          </div>
        )}
        <span
          className="relative text-[16px] leading-none"
          style={{
            color: SIGNATURE,
            animation: running
              ? "star-pulse 1.5s ease-in-out infinite"
              : "none",
          }}
        >
          ✦
        </span>
      </div>
    );
  }

  // remember — settling/landing animation
  return (
    <div
      className="relative flex items-center justify-center"
      style={{ width: 40, height: 40 }}
    >
      {/* Progress ring */}
      <svg
        className="absolute inset-0"
        width={40}
        height={40}
        viewBox="0 0 40 40"
      >
        <circle
          cx={20}
          cy={20}
          r={16}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          className="text-foreground/8"
        />
        {running && (
          <circle
            cx={20}
            cy={20}
            r={16}
            fill="none"
            strokeWidth={1.5}
            strokeLinecap="round"
            style={{
              stroke: SIGNATURE,
              strokeDasharray: circumference,
              strokeDashoffset: circumference * (1 - progress),
              transform: "rotate(-90deg)",
              transformOrigin: "center",
              transition: "stroke-dashoffset 0.3s ease",
            }}
          />
        )}
      </svg>
      <span
        className="relative text-[16px] leading-none"
        style={{
          color: SIGNATURE,
          animation: running ? "star-glow 2.5s ease-in-out infinite" : "none",
        }}
      >
        ✦
      </span>
    </div>
  );
}

function TimeAwareLoadingDemo({
  mode,
}: {
  mode: "recall" | "ask" | "remember";
}) {
  const [running, setRunning] = useState(false);
  const [currentMsg, setCurrentMsg] = useState("");
  const [prevMsg, setPrevMsg] = useState("");
  const [fading, setFading] = useState(false);
  const [stepIndex, setStepIndex] = useState(0);
  const [elapsed, setElapsed] = useState(0);
  const rafRef = useRef<number>(0);
  const startRef = useRef(0);
  const lastMsgRef = useRef("");

  const messages = MESSAGES[mode];
  const totalDuration =
    THRESHOLDS[Math.min(messages.length - 1, THRESHOLDS.length - 1)] + 3000;

  const stop = useCallback(() => {
    setRunning(false);
    cancelAnimationFrame(rafRef.current);
    setCurrentMsg("");
    setPrevMsg("");
    setFading(false);
    setStepIndex(0);
    setElapsed(0);
    lastMsgRef.current = "";
  }, []);

  useEffect(() => {
    if (!running) return;

    startRef.current = performance.now();
    lastMsgRef.current = "";

    const step = (now: number) => {
      const elapsedMs = now - startRef.current;
      setElapsed(elapsedMs);

      // Determine current message index based on thresholds
      let msgIdx = 0;
      for (let i = THRESHOLDS.length - 1; i >= 0; i--) {
        if (elapsedMs >= THRESHOLDS[i] && i < messages.length) {
          msgIdx = i;
          break;
        }
      }

      const targetMsg = messages[msgIdx];
      setStepIndex(msgIdx);

      // Crossfade between messages
      if (targetMsg !== lastMsgRef.current) {
        if (lastMsgRef.current !== "") {
          setPrevMsg(lastMsgRef.current);
          setFading(true);
          // After fade-out, swap to new message
          setTimeout(() => {
            setCurrentMsg(targetMsg);
            setFading(false);
          }, 300);
        } else {
          setCurrentMsg(targetMsg);
        }
        lastMsgRef.current = targetMsg;
      }

      // Stop after all messages have played out
      if (elapsedMs > totalDuration) {
        stop();
        return;
      }

      rafRef.current = requestAnimationFrame(step);
    };

    rafRef.current = requestAnimationFrame(step);
    return () => cancelAnimationFrame(rafRef.current);
  }, [running, messages, stop, totalDuration]);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <StarIndicator
          mode={mode}
          running={running}
          elapsed={elapsed}
          totalDuration={totalDuration}
        />
        <div className="relative min-w-50 h-5">
          {/* Previous message (fading out) */}
          {fading && prevMsg && (
            <span
              className="absolute inset-0 text-[14px] text-fg-2"
              style={{
                opacity: 0,
                transition: "opacity 0.3s ease",
              }}
            >
              {prevMsg}
            </span>
          )}
          {/* Current message (fading in) */}
          <span
            className="absolute inset-0 text-[14px] text-fg-2"
            style={{
              opacity: fading ? 0 : 1,
              transition: "opacity 0.3s ease",
            }}
          >
            {currentMsg}
          </span>
        </div>
      </div>
      <div className="flex items-center gap-3 pl-13">
        <button
          onClick={() => (running ? stop() : setRunning(true))}
          className="text-[12px] px-3 py-1 rounded-md border border-border/60 text-fg-2 hover:text-fg-2 transition-colors"
        >
          {running ? "Stop" : "Play"}
        </button>
        {running && (
          <span className="text-[10px] text-fg-4">
            step {stepIndex + 1}/{messages.length}
          </span>
        )}
      </div>
    </div>
  );
}

export function LoadingTextSection() {
  return (
    <Section
      title="5. Loading Text"
      description="Time-progressive loading with animated star indicators. Each mode has a unique visual identity: sonar rings (recall), pulse dots (ask), settling glow (remember)."
    >
      {/* Inject keyframes */}
      <style dangerouslySetInnerHTML={{ __html: loadingKeyframes }} />

      <div className="grid grid-cols-1 gap-4">
        <DemoCard label="Recall mode &mdash; sonar rings (searching memory)">
          <TimeAwareLoadingDemo mode="recall" />
        </DemoCard>

        <DemoCard label="Ask mode &mdash; pulse dots (thinking)">
          <TimeAwareLoadingDemo mode="ask" />
        </DemoCard>

        <DemoCard label="Remember mode &mdash; settling glow (placing memory)">
          <TimeAwareLoadingDemo mode="remember" />
        </DemoCard>
      </div>
    </Section>
  );
}
