import type { ComponentType, SVGProps } from "react";
import {
  ClaudeCodeMark,
  ClaudeMark,
  CodexMark,
  CopilotMark,
  CursorMark,
  GeminiMark,
  OpenClawMark,
  MuseMark,
  HermesMark,
  OpenCodeMark,
  WindsurfMark,
} from "../marks/brand-marks";

export type BrandMarkComponent = ComponentType<SVGProps<SVGSVGElement>>;

/**
 * Maps agent slug → brand-mark component for landing / marketing surfaces.
 *
 * Parallel to AGENT_IDENTITIES (in-app lucide icons keyed by the same
 * slugs). Landing uses these for brand recognition at 20px+; in-app uses
 * AGENT_IDENTITIES for scannable color-coded shapes at 16px in memory
 * rows. Different jobs, different assets — see marks/brand-marks.tsx.
 *
 * Marks vendored verbatim from lobehub/lobe-icons (MIT), except Muse
 * (Meta's official favicon mark, flattened to monochrome).
 */
export const AGENT_BRAND_MARKS: Record<string, BrandMarkComponent> = {
  "claude-code": ClaudeCodeMark,
  "claude-ai": ClaudeMark,
  cursor: CursorMark,
  codex: CodexMark,
  copilot: CopilotMark,
  windsurf: WindsurfMark,
  gemini: GeminiMark,
  openclaw: OpenClawMark,
  muse: MuseMark,
  hermes: HermesMark,
  opencode: OpenCodeMark,
};
