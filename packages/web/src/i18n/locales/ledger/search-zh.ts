// 跨空间搜索（Search.png）的中文文案，挂在 `t.ledger.search` 下，键与
// ./search-en.ts 一一对应。
import type { Translations } from "../en";

export const ledgerSearchZh: Translations["ledger"]["search"] = {
  field: "搜索全部",
  placeholder: "搜索记忆、决策和笔记",
  askInstead: "改为提问：",
  askLabel: "改为向 {space} 提问：{question}",
  facets: {
    label: "筛选",
    kind: "类型",
    state: "状态",
    space: "空间",
    writtenBy: "写入者",
    author: "{name} 写下的",
  },
  kinds: {
    memories: "记忆",
    decisions: "决策",
    notes: "原始笔记",
  },
  states: {
    kept: "已保留",
    waiting: "等你处理",
    stale: "过时",
  },
  results: "“{query}”在 {where} 中有 {n} 条结果",
  resultsOne: "“{query}”在 {where} 中有 1 条结果",
  best: "“{query}”在 {where} 中最相关的 {n} 条结果",
  spaces: "{n} 个空间",
  hidden: "已隐藏 {n} 条原始笔记",
  hiddenOne: "已隐藏 1 条原始笔记",
  decisionsIn: "{space} 中 {n} 条",
  more: "另外 {n} 条",
  fewer: "收起",
  open: "未定",
  inForce: "生效中",
  proposed: "提议",
  stale: "过时",
  start: {
    title: "搜索你的空间。",
    detail: "你所在的每个空间里的记忆、决策，以及你的原始笔记。",
  },
  none: {
    title: "没有与“{query}”匹配的内容。",
    detail: "换个说法，或者直接提问：Memax 会根据已保留的内容作答并注明出处。",
  },
  filtered: {
    title: "按这些筛选条件，这里没有内容。",
    detail: "“{query}”的 {n} 条结果被筛掉了。",
    detailOne: "“{query}”的 1 条结果被筛掉了。",
    reset: "显示全部",
  },
  loading: "正在搜索你的空间",
  loadingMeta: "搜索中",
  everywhere: "搜索你的所有空间",
};
