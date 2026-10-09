// @vitest-environment jsdom
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { AgentListHead, AgentRow } from "../agents/agent-row";
import { DecisionGate } from "../agents/decision-gate";
import { HandoffSlip } from "../agents/handoff-slip";
import { SyncTarget } from "../agents/sync-target";
import { Icon } from "../brand/icon";
import { Logo } from "../brand/logo";
import { Seal } from "../brand/seal";
import { CommandBar } from "../frame/command-bar";
import { PageHeader } from "../frame/page-header";
import { Shell } from "../frame/shell";
import { Terminal } from "../frame/terminal";
import { Diff } from "../memory/diff";
import { DreamCard } from "../memory/dream-card";
import { Lineage } from "../memory/lineage";
import { MemoryList } from "../memory/memory-list";
import { MemoryRow } from "../memory/memory-row";
import { MemoryText } from "../memory/memory-text";
import { Redaction } from "../memory/redaction";
import { ReviewCard } from "../memory/review-card";
import { StateMark } from "../memory/state-mark";
import { Button } from "../primitives/button";
import { Field } from "../primitives/field";
import { Kbd } from "../primitives/kbd";
import { Segmented } from "../primitives/segmented";
import { AgentStamp } from "../provenance/agent-stamp";
import { Cite } from "../provenance/cite";
import { Highlight } from "../provenance/highlight";
import { Receipt } from "../provenance/receipt";
import type { MarkState } from "../lib/types";
import { LedgerProvider } from "./provider";

// No hard-coded English inside the components: with zh strings and Chinese
// data, nothing a person sees or hears is English, except proper nouns,
// product terms that stay in English, keys and IDs.

const ALLOWED = new Set([
  // Agent names and monograms (never translated), people's initials.
  ..."Claude Code Codex Cursor ChatGPT Gemini CLI Copilot OpenCode Dream".split(
    " ",
  ),
  ..."CC CX CU GPT CL GM CP OC DR ZZ JY WI".split(" "),
  // Product terms kept in English in V1's Chinese copy, and the product name.
  ..."Agent MCP CLI IDE Memax memax".split(" "),
  // Keys.
  ..."Esc Tab".split(" "),
  // English proper nouns and file names inside the Chinese test data itself.
  ..."agent River Postgres pnpm catalog Haiku windsurf AGENTS CLAUDE md".split(
    " ",
  ),
]);
const ID = /^(M|N|H|PR)$/;

function englishIn(root: HTMLElement): string[] {
  const parts: string[] = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    parts.push(node.textContent ?? "");
  }
  for (const el of root.querySelectorAll("*")) {
    for (const attr of ["aria-label", "title", "placeholder", "alt"]) {
      const value = el.getAttribute(attr);
      if (value) parts.push(value);
    }
  }
  const words = parts.join(" ").match(/[A-Za-z]{2,}/g) ?? [];
  return [...new Set(words.filter((w) => !ALLOWED.has(w) && !ID.test(w)))];
}

const STATES: MarkState[] = [
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

const CASES: Array<[string, ReactNode]> = [
  [
    "Icon, Logo, Seal",
    <>
      <Icon name="shield" />
      <Logo />
      <Logo variant="mark" />
      <Seal date="10月2日" id="M-0219" size={112} />
    </>,
  ],
  [
    "Button",
    <>
      <Button kbd="K" variant="keep">
        保留
      </Button>
      <Button disabled disabledReason="先选一个">
        回答
      </Button>
      <Button href="/x">链接</Button>
    </>,
  ],
  ["Kbd", <Kbd>⌘K</Kbd>],
  [
    "Field",
    <Field label="空间名称" hint="agent 会在收据里看到它。" error="太长了。" />,
  ],
  [
    "Segmented",
    <Segmented
      label="筛选"
      options={[
        { value: "a", label: "全部" },
        { value: "b", label: "冲突" },
      ]}
    />,
  ],
  [
    "AgentStamp",
    <>
      <AgentStamp agent="codex" showName surface />
      <AgentStamp agent="dream" />
      <AgentStamp person="ZZ" name="你" showName surface />
      <AgentStamp agent="windsurf" />
    </>,
  ],
  [
    "Receipt",
    <Receipt
      agent="codex"
      action="提议"
      time="14:02"
      source="会话 8f2c"
      id="M-0431"
    />,
  ],
  [
    "Cite and Highlight",
    <p>
      <Highlight>River 跑在 Postgres 上</Highlight>。
      <Cite n={1} title="M-0219" href="#M-0219" />
      <Cite n={2} />
    </p>,
  ],
  [
    "StateMark",
    <>
      {STATES.map((s) => (
        <StateMark key={s} state={s} />
      ))}
      {STATES.map((s) => (
        <StateMark key={`${s}-g`} state={s} label={false} />
      ))}
    </>,
  ],
  [
    "MemoryRow",
    <MemoryList>
      <MemoryRow
        state="proposed"
        agent="codex"
        time="22 分钟前"
        id="M-0432"
        space="memax-v2"
        href="#x"
        actions={<Button icon="pencil" aria-label="编辑" />}
      >
        用 pnpm catalog 固定共享依赖版本。
      </MemoryRow>
      <MemoryRow state="stale" agent="dream" note="来源在 9月11日变了">
        回答用 Haiku 档。
      </MemoryRow>
      <MemoryRow onClick={() => {}} person="ZZ" name="你">
        后台任务跑在 River 上。
      </MemoryRow>
    </MemoryList>,
  ],
  [
    "MemoryText",
    <MemoryText state="proposed">后台任务跑在 River 上。</MemoryText>,
  ],
  [
    "Redaction",
    <MemoryList>
      <Redaction date="10月3日" id="M-0388" />
      <Redaction date="9月29日" by="佳豪" />
    </MemoryList>,
  ],
  ["Diff", <Diff before="部署到 甲" after="部署到 乙" />],
  [
    "Lineage",
    <Lineage
      events={[
        {
          state: "kept",
          person: "ZZ",
          title: "由你保留",
          time: "10月2日 10:58",
        },
      ]}
    />,
  ],
  [
    "ReviewCard (proposed, external, conflict, update)",
    <ReviewCard
      kept={false}
      agent="codex"
      time="1 小时前"
      id="M-0430"
      space="memax-v2"
      source="会话 3e1a"
      statement="写入要先确认。"
      before="写入要先询问。"
      beforeId="M-0156"
      external="Claude Code 是在 MCP 规范里读到的。"
      conflictWith="部署到另一处。"
      evidence="“需要输入时返回。”"
      evidenceSource="规范"
    />,
  ],
  [
    "ReviewCard (kept)",
    <ReviewCard
      kept
      agent="codex"
      id="M-0432"
      statement="固定版本。"
      keptBy="ZZ"
      keptDate="10月5日"
      onUndo={() => {}}
    />,
  ],
  [
    "DreamCard",
    <DreamCard
      issue={214}
      date="10月5日 周一"
      time="03:12"
      duration="41 秒"
      notes={34}
      facts={6}
      noteIds={["N-1180", "N-1181"]}
      factIds={["M-0219"]}
      items={[
        {
          kind: "merged",
          text: "后台任务跑在 River 上。",
          meta: "9 条笔记 → M-0219",
        },
        { kind: "conflict", text: "部署到哪？", meta: "需要你" },
        { kind: "faded", text: "11 条笔记 60 天没人读。" },
      ]}
    />,
  ],
  [
    "HandoffSlip",
    <>
      {(["drafted", "sent", "accepted"] as const).map((status) => (
        <HandoffSlip
          key={status}
          id="H-0093"
          from="claude-code"
          to="codex"
          toSurface="云端任务"
          status={status}
          title="完成迁移"
          done={["去掉握手。"]}
          next={["返回确认。"]}
          questions={["部署到哪？"]}
          carries="7 条记忆"
          time="14:20 发出"
        />
      ))}
    </>,
  ],
  [
    "AgentRow",
    <div className="mx-panel">
      <AgentListHead />
      <AgentRow
        agent="claude-code"
        autonomy="write"
        reads={1204}
        writes={38}
        lastSeen="2 分钟前"
        target="CLAUDE.md"
      />
      <AgentRow agent="gemini" autonomy="read" reads={0} writes={0} paused />
      <AgentRow agent="codex" autonomy="propose" reads={0} writes={0} />
    </div>,
  ],
  [
    "SyncTarget",
    <>
      {(["synced", "drifted", "pending", "off"] as const).map((status) => (
        <SyncTarget
          key={status}
          path="AGENTS.md"
          tool="Codex"
          status={status}
          detail="214 条记忆"
        />
      ))}
    </>,
  ],
  [
    "DecisionGate",
    <>
      <DecisionGate
        agent="codex"
        question="用哪个？"
        options={[{ label: "甲" }, { label: "乙" }]}
        space="memax-v2"
      />
      <DecisionGate
        agent="codex"
        question="用哪个？"
        options={[{ label: "甲" }]}
      />
    </>,
  ],
  [
    "CommandBar",
    <CommandBar defaultQuery="为什么选 River？">
      <p className="mx-answer">因为 Postgres。</p>
    </CommandBar>,
  ],
  ["CommandBar (remember)", <CommandBar defaultMode="remember" />],
  [
    "Shell, NavRail and PageHeader",
    <Shell
      nav={{
        active: "review",
        space: "memax-v2",
        spaceKind: "项目",
        person: "ZZ",
        settingsHref: "#s",
        status: { state: "kept", label: "5 个 agent 已同步" },
        items: [
          { id: "today", href: "#t" },
          { id: "review", href: "#r", count: 5, tone: "pending" },
          { id: "briefs", href: "#b" },
          { id: "memories", href: "#m" },
          { id: "handoffs", href: "#h" },
          { id: "agents", href: "#a" },
          { id: "decisions", href: "#d" },
        ],
      }}
    >
      <PageHeader
        title="今天"
        lede="有 4 条提议在等你。"
        eyebrow="memax-v2 · 项目空间"
      />
    </Shell>,
  ],
  [
    "Terminal",
    <Terminal
      title="终端"
      lines={[
        { kind: "ok", text: "完成" },
        { kind: "kept", text: "M-0174" },
        { kind: "proposed", text: "M-0431" },
        { kind: "warn", text: "注意" },
        { kind: "forgotten", text: "已忘记" },
        { kind: "blank" },
      ]}
    />,
  ],
];

describe("Chinese coverage", () => {
  it.each(CASES)("%s shows and announces no English", (_, node) => {
    const { container } = render(
      <LedgerProvider locale="zh">
        <div lang="zh-CN">{node}</div>
      </LedgerProvider>,
    );
    expect(englishIn(container)).toEqual([]);
  });

  it("would catch English", () => {
    const { container } = render(
      <LedgerProvider locale="en">
        <StateMark state="proposed" />
      </LedgerProvider>,
    );
    expect(englishIn(container)).toEqual(["Proposed"]);
  });
});
