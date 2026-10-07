// 授权 Agent 连接（OAuthConsent，plan 25 §5.15）的中文文案。挂在
// `t.ledger.consent` 下，键与 ./consent-en.ts 一一对应。{client} 是客户端
// 给自己起的名字。
import type { Translations } from "../en";

export const ledgerConsentZh: Translations["ledger"]["consent"] = {
  pageTitle: "连接 Agent",
  title: "{client} 想连接到 Memax",
  signedInAs: "已登录：{name}",
  notYou: "不是你？",
  servedFrom: "来自 {host}",
  servedFromLabel: "Memax 从 {host} 读取了这个 Agent 的信息",
  which: "连接哪个空间",
  meta: {
    project: "项目",
    projectMeta: "项目 · {memories}",
    team: "团队",
    teamMeta: "团队 · {people}",
    personal: "只有你",
    memories: "{n} 条记忆",
    memoriesOne: "1 条记忆",
    people: "{n} 人",
    peopleOne: "1 人",
    onV1: "{meta} · 还在 V1 上",
  },
  compiles: "编译到 {file}",
  disabled: "你在这里的角色用不了 {client} 要的权限。",
  will: "{client} 可以",
  wont: "它不能",
  can: {
    read_brief: "读取简报和已保留的记忆",
    read_memories: "读取这里的记忆",
    propose: "提议记忆，等你来定",
    keep: "不问你就保留记忆",
    add: "添加记忆，写下就直接保留",
    gate: "遇到岔路时请你来决定",
    forget: "忘记记忆",
    other_spaces: "查看你的其他空间",
  },
  cannot: {
    read_brief: "读取简报",
    read_memories: "读取这里的记忆",
    propose: "提议记忆",
    keep: "不经你同意保留任何内容",
    add: "添加记忆",
    gate: "遇到岔路时请你来决定",
    forget: "忘记任何内容",
    other_spaces: "查看你的其他空间",
  },
  cancel: "取消",
  allow: "允许 {client}",
  footnote: "你随时可以在 Agent 页面允许写入，或断开 {client}。",
  footnoteRead: "你随时可以在 Agent 页面让 {client} 提议或写入，或者断开它。",
  footnoteV1: "你随时可以在 Agent 页面断开 {client}。",
  errors: {
    space: "这个账号连接不了那个空间。从这里列出的空间里选一个吧。",
    permission: "Memax 给不了 {client} 要的权限。从 {client} 重新发起连接吧。",
  },
  loading: "正在读取请求",
  missing: {
    title: "这个链接里没有请求。",
    lede: "从你的 Agent 重新发起连接，它会带着请求打开这个页面。",
  },
  expired: {
    title: "这个请求过期了。",
    lede: "请求的有效期是 10 分钟。从你的 Agent 重新发起连接吧。",
  },
  gone: {
    title: "这个请求已经结束了。",
    lede: "它已经处理过了，或者这是个旧链接。如果你的 Agent 还在等，就从它那边重新发起连接。",
  },
  failed: {
    title: "这个请求没能加载出来。",
    lede: "检查一下网络，再试一次。请求的有效期是 10 分钟。",
    retry: "再试一次",
  },
  noSpaces: {
    title: "你还没有空间。",
    lede: "先建一个，然后从你的 Agent 重新连接 {client}。",
    setup: "设置 Memax",
  },
  fallbackClient: "一个 Agent",
};
