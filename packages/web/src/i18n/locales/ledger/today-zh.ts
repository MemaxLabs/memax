// "今天"页的中文文案，挂在 `t.ledger.today` 下，键与 ./today-en.ts
// 一一对应。页面标题、导语和操作在 app-zh.ts（`t.ledger.app.today`）。
import type { Translations } from "../en";

export const ledgerTodayZh: Translations["ledger"]["today"] = {
  dream: {
    seconds: "{n} 秒",
    folded: "{n} 条笔记 → {ref}",
    needsYou: "需要你",
    restorable: "可恢复",
    none: "还没有晨报",
    noneDetail:
      "每天早上，Dream 会在这里发一期晨报：它把哪些笔记整理成了事实、发现了哪些冲突、哪些淡出了。这个空间还没有开始运行 Dream。",
    quiet: "昨晚没有晨报",
    quietDetail: "Dream 没有新东西可读，所以没有晨报。空着的夜晚不花钱。",
  },
  waiting: {
    title: "等你处理",
    count: "{n} 条在等你",
    proposals: "{n} 条提议",
    proposalsOne: "1 条提议",
    verify: "{n} 条待核对",
    more: "审阅里还有 {n} 条",
    moreOne: "审阅里还有 1 条",
    open: "打开审阅",
    nothing: "没有在等你的事。",
    question: "一个问题",
    questions: "{n} 个问题",
    asked: "{n} 个问题",
    askedOne: "1 个问题",
  },
  inFlight: {
    title: "进行中",
    working: "{agent} 正在处理",
    questions: "有 {n} 个问题要问你",
    questionsOne: "有 1 个问题要问你",
    from: "从 {from} 交给 {to}",
    none: "没有进行中的交接。",
    later:
      "暂时还不能交接。等 Agent 能把会话交出去时，进行中的交接会显示在这里。",
  },
  agents: {
    title: "今天的 Agent",
    active: "{total} 个里有 {n} 个在用",
    read: "读取 {n} 次",
    kept: "保留 {n} 条",
    proposed: "提议 {n} 条",
    none: "没有写入",
    nobody: "今天还没有 Agent 在这里工作。",
    noAgents: "还没有连接 Agent。",
    connect: "连接 Agent",
    unavailable: "Agent 没能载入。",
  },
  compiled: {
    title: "编译出的上下文",
    recompile: "重新编译所有文件",
    none: "还没有要编译的文件。",
  },
  footer: {
    compiledAt: "编译于 {time}",
    compiledOn: "编译于 {date} {time}",
    dreamAt: "Dream 每晚 {time} 运行",
    kept: "{space} 里保留了 {n} 条记忆",
    keptOne: "{space} 里保留了 1 条记忆",
  },
  empty: {
    title: "这里还什么都没有",
    lede: "连接一个 Agent，Memax 会读一读它对 {where} 已经知道的内容。所有内容都先作为提议进来。",
    steps: "三步完成第一次编译",
    connect: "在这个仓库里连接 Agent",
    connectAnywhere: "连接 Agent",
    connectDetail:
      "在 {repository} 里运行这条命令。它会找到 Claude Code、Codex、Cursor 等。",
    connectDetailAnywhere:
      "在你的仓库里运行这条命令。它会找到 Claude Code、Codex、Cursor 等。",
    command: "npx memax-cli init --space {space}",
    copy: "复制命令",
    connectHere: "或者在这里连接",
    settle: "把它们的分歧定下来",
    settleDetail: "第一天的整理会指出它们的文件在哪里互相矛盾。",
    compile: "保留对的，然后编译",
    compileDetail:
      "Memax 会把 AGENTS.md、导入它的 CLAUDE.md 和 Cursor 规则写进仓库。",
    done: "完成",
    startFrom: "或者从这里开始",
    conventionsFrom: "沿用 {space} 的约定",
    conventionsLater: "暂时还不能从别的空间开始。",
    drop: "拖入 CLAUDE.md 或 AGENTS.md",
    dropMeta: "读成提议，不会一进来就保留",
    dropLater:
      "暂时还不能读拖进来的文件。`npx memax-cli init` 会从仓库里读取它们。",
    dreamTonight: "等有内容可读，Dream 今晚 {time} 就会运行。",
  },
};
