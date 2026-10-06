// V2（Ledger）中文文案。挂在 ../zh.ts 的 `t.ledger` 下，键与 ./en.ts 一一对应。
// 语气：平实、准确、冷静。产品名写作 "Memax"（V2 规则）。
// 不用"AI""智能""魔法""删除""保存""批准"，不用感叹号和表情。
import type { Translations } from "../en";
import { ledgerAppZh } from "./app-zh";
import { ledgerMemoryZh } from "./memory-zh";
import { ledgerRecordsZh } from "./records-zh";
import { ledgerReviewZh } from "./review-zh";

export const ledgerZh: Translations["ledger"] = {
  meta: {
    description: "属于你的上下文层。每一条保留，都有收据。",
  },
  theme: {
    label: "主题",
    light: "Paper 浅色",
    dark: "Carbon 深色",
    system: "跟随系统",
  },
  notFound: {
    title: "这个地址没有页面。",
    description:
      "检查一下链接。如果是别人发给你的，它可能指向一个你不在的空间。",
    home: "回到 Memax",
  },
  error: {
    title: "这个页面没能加载出来。",
    description: "你保留的内容都还在。再试一次；如果一直这样，就刷新页面。",
    retry: "再试一次",
    home: "回到 Memax",
    digest: "错误 {digest}",
  },
  app: ledgerAppZh,
  records: ledgerRecordsZh,
  review: ledgerReviewZh,
  memory: ledgerMemoryZh,
  devTokens: {
    breadcrumb: "Ledger · 开发样张",
    title: "设计令牌与字体",
    lede: "Ledger 的每个令牌和字体样式，Paper 与 Carbon 并排对照。这一页是视觉回归测试的第一个基准。",
    switchToV1: "切换到 V1",
    sections: {
      colour: "颜色",
      type: "字体",
      space: "间距",
      radius: "圆角",
      size: "尺寸",
      elevation: "阴影",
      states: "状态标记",
    },
    statesNote:
      "SVG 资源按 Paper 的颜色绘制，给邮件和文档用。在应用里，StateMark 会跟随主题。",
    states: {
      proposed: "待确认",
      kept: "已保留",
      merged: "已合并",
      stale: "已过时",
      faded: "已淡出",
      conflict: "有冲突",
      forgotten: "已忘记",
    },
    samples: {
      wordmark: "Memax",
      display: "一份上下文，每个 agent 都在读。",
      title: "10 月 5 日，星期一",
      heading: "决定",
      "memory-lg": "后台任务跑在 River 上，基于 Postgres。我们不用 Temporal。",
      memory: "API 错误统一用 RFC 9457 problem+json。",
      "memory-proposed": "把 v2 API 部署到 Fly.io 的 iad 和 ams。",
      prose: "Memax V2 是团队里每个 agent 写代码之前都会先读的上下文层。",
      "ui-title": "等你处理",
      ui: "Codex 在 memax-v2 里提议了 3 条记忆",
      "ui-strong": "保留",
      "ui-sm": "Cursor 14 分钟前读过",
      label: "自主程度",
      receipt: "CX 提议 · 14:02 · 会话 8f2c · M-0412",
      "receipt-strong": "M-0412",
      code: "npx memax-cli init",
    },
  },
};
