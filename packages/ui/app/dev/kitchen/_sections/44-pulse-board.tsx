"use client";

import { useState } from "react";
import {
  BoardAction,
  BoardActionRow,
  BoardAgentRow,
  BoardCard,
  BoardCardFallbackBody,
  BoardCiteChip,
  BoardKindLabel,
  BoardMemQuote,
  BoardReceipt,
  BoardSlotStrip,
  BoardVoiceStar,
  type BoardCardState,
} from "../../../../src";
import { AGENT_IDENTITIES } from "@/tokens/agents";
import { Section, DemoCard } from "../_shared";

// Agent dots come from the canonical identity tokens — the same colors
// the product uses for memory-row attribution. Only the idea-category
// dot is board-specific.
const DOT_CLAUDE = AGENT_IDENTITIES["claude-code"].color;
const DOT_CODEX = AGENT_IDENTITIES["codex"].color;
const DOT_IDEA = "#8b5cf6";

/**
 * 44. Pulse board — L1 atoms + L2 card molecule (plan 25, P0).
 *
 * These are the REAL components from @memaxlabs/ui (board-atoms.tsx /
 * board-card.tsx), not mocks: what renders here is exactly what the
 * web app mounts. Northstar reference: the pulse-board-design demo.
 */
export function PulseBoardSection() {
  return (
    <Section
      title="44. Pulse board"
      description="L1 atoms and the BoardCard lifecycle molecule. Every board kind — 行迹, 回声, 等你, 梦记, custom — assembles from these eight pieces. Component map: ui/src/components/board-atoms.tsx + board-card.tsx."
    >
      <AtomSpecimens />
      <LifecycleDemo />
      <FallbackDemo />
    </Section>
  );
}

function AtomSpecimens() {
  const [peek, setPeek] = useState(false);
  const [stripOpen, setStripOpen] = useState(false);
  return (
    <DemoCard label="44-atoms. The eight L1 atoms">
      <div className="grid gap-6 md:grid-cols-2">
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">
            KindLabel + VoiceStar
          </p>
          <BoardKindLabel star>回声 · 118 天前的问题，有答案了</BoardKindLabel>
          <BoardKindLabel dotColor={DOT_IDEA}>暗线</BoardKindLabel>
          <BoardKindLabel>行迹 · 你不在的 9 小时</BoardKindLabel>
          <p className="text-[12px] text-fg-3">
            ✦ 只给 memax 第一人称发言的 kind（
            <BoardVoiceStar breathing />
            <span className="text-fg-2">呼吸态用于 live 状态</span>
            ）；观察类 kind 不加星 — 稀缺性就是规范。
          </p>
        </div>
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">MemQuote</p>
          <BoardMemQuote when="6 月 2 日">
            “staging 部署走手动 fly deploy”
          </BoardMemQuote>
          <BoardMemQuote
            when="8 月 4 日 · 你的决策"
            onClick={() => setPeek((v) => !v)}
          >
            “staging 由 CI 在 main 合并后自动部署”（点我）
          </BoardMemQuote>
        </div>
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">CiteChip</p>
          <div className="flex flex-wrap gap-1.5">
            <BoardCiteChip
              dotColor={DOT_IDEA}
              label="3 月 14 日 · 闪念"
              active={peek}
              onClick={() => setPeek((v) => !v)}
            />
            <BoardCiteChip dotColor={DOT_CLAUDE} label="8 月 4 日 · 决策" />
          </div>
          {peek ? (
            <BoardMemQuote when="3 月 14 日 · 闪念">
              “如果 persona 是可以携带的，那 agent 只是它暂住的身体…”
            </BoardMemQuote>
          ) : null}
        </div>
        <div className="space-y-1">
          <p className="text-[11px] font-mono text-fg-4">AgentRow</p>
          <BoardAgentRow
            dotColor={DOT_CLAUDE}
            title="推了 12 条记忆"
            meta="memax 迁移 · 决策 ×3 · 修复 ×2"
            who="Claude Code"
          />
          <BoardAgentRow
            dotColor={DOT_CODEX}
            title="查了 4 次部署配置"
            meta="全部命中缓存"
            who="Codex"
          />
        </div>
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">ActionRow + Action</p>
          <BoardActionRow className="mt-0">
            <BoardAction emphasis="primary">合上这个环</BoardAction>
            <BoardAction>展开看</BoardAction>
            <BoardAction emphasis="quiet">先放着</BoardAction>
          </BoardActionRow>
        </div>
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">SlotStrip</p>
          <BoardSlotStrip
            label="项目脉搏"
            detail={stripOpen ? "收起" : "3 个项目有动静"}
            open={stripOpen}
            onToggle={() => setStripOpen((v) => !v)}
          />
          {stripOpen ? (
            <BoardAgentRow
              dotColor={DOT_CODEX}
              title="pulse-p0 分支 · 19 次提交"
              meta="boards 三表 + 八原子落地"
              who="今天"
            />
          ) : null}
        </div>
        <div className="space-y-2">
          <p className="text-[11px] font-mono text-fg-4">Receipt</p>
          <BoardReceipt>已收下 · 今天 7:41</BoardReceipt>
          <BoardReceipt>已互链 · 写回了一条关系记忆</BoardReceipt>
        </div>
      </div>
    </DemoCard>
  );
}

function LifecycleDemo() {
  const [state, setState] = useState<BoardCardState>("fresh");
  return (
    <DemoCard label="44-card. BoardCard lifecycle — fresh → resolved / dismissed">
      <div className="mx-auto max-w-sm space-y-3">
        <BoardCard
          state={state}
          live={
            <BoardActionRow>
              <BoardAction
                emphasis="primary"
                onClick={() => setState("resolved")}
              >
                都对 · 收下
              </BoardAction>
              <BoardAction
                emphasis="quiet"
                onClick={() => setState("dismissed")}
              >
                不关心
              </BoardAction>
            </BoardActionRow>
          }
          receipt={state === "resolved" ? "已收下 · 刚刚" : "已略过 · 刚刚"}
        >
          <BoardKindLabel>行迹 · 你不在的 9 小时</BoardKindLabel>
          <BoardAgentRow
            dotColor={DOT_CLAUDE}
            title="推了 12 条记忆"
            meta="memax 迁移 · 决策 ×3"
            who="Claude Code"
          />
          <BoardAgentRow
            dotColor={DOT_CODEX}
            title="查了 4 次部署配置"
            meta="全部命中缓存"
            who="Codex"
          />
        </BoardCard>
        <button
          type="button"
          onClick={() => setState("fresh")}
          className="text-[12px] text-fg-4 hover:text-fg-2"
        >
          重置为 fresh
        </button>
        <p className="text-[12px] text-fg-3">
          终态卡不消失：live 区换成 Receipt，边框转虚线（dismissed
          额外降不透明度），直到槽位被新内容替换。
        </p>
      </div>
    </DemoCard>
  );
}

function FallbackDemo() {
  return (
    <DemoCard label="44-fallback. Unknown-kind fallback — producer ships ahead of renderer">
      <div className="mx-auto max-w-sm">
        <BoardCard
          state="fresh"
          live={
            <BoardActionRow>
              <BoardAction emphasis="quiet">收下</BoardAction>
            </BoardActionRow>
          }
        >
          <BoardKindLabel>quantum_insight</BoardKindLabel>
          <BoardCardFallbackBody
            title="这个 kind 的渲染器还没上线"
            description="payload 里的 title/description 是纯文本，所以旧客户端也能原样读到内容 — plan-18 §4.2 的契约延续到 board。"
          />
        </BoardCard>
      </div>
    </DemoCard>
  );
}
