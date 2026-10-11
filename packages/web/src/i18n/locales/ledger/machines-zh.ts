// 个人空间写到哪些电脑、写进哪些文件的中文文案，挂在 `t.ledger.machines`
// 下，键与 ./machines-en.ts 一一对应。
import type { Translations } from "../en";

export const ledgerMachinesZh: Translations["ledger"]["machines"] = {
  canonical: "主副本",
  platform: {
    darwin: "macOS",
    linux: "Linux",
    win32: "Windows",
    windows: "Windows",
  },
  status: {
    inSync: "{n} 台电脑上都已同步",
    inSyncOn: "{machine} 上已同步",
    drifted: "{machine} 上有手动改动",
    driftedMany: "{n} 台电脑上有手动改动",
    held: "{machine} 上的文件在等审阅",
    heldMany: "{n} 台电脑上的文件在等审阅",
    compiling: "编译中",
    behind: "{of} 台电脑里 {reached} 台已同步",
    waitingFor: "等 {machine} 写入",
    machineSeen: "{machine} {seen}",
    off: "已停止",
  },
  state: {
    in_sync: "已同步",
    compiling: "编译中",
    waiting: "等后台程序写入",
    offline: "离线",
    drifted: "有手动改动",
    held: "等审阅",
    stopped: "已停止",
    unlinked: "已解除关联",
  },
  seen: {
    now: "现在在线",
    minutes: "{n} 分钟前在线",
    hours: "{n} 小时前在线",
    yesterday: "昨天在线",
    days: "{n} 天前在线",
    on: "{date} 在线",
    never: "还没上过线",
  },
  panel: {
    title: "电脑",
    titleAccount: "个人空间写在哪些电脑上",
    linked: "已关联 {n} 台",
    linkedOne: "已关联 1 台",
    none: "你的个人空间还没写到任何电脑上。在你用的每台电脑上运行：",
    another: "在另一台电脑上运行这条命令，也把个人空间写过去：",
    copy: "复制命令",
    noFiles: "没有 Agent 的文件",
    loadFailed: "电脑列表没能载入。",
    retry: "重试",
    rename: "重命名",
    renameAria: "重命名 {machine}",
    renameLabel: "{machine} 的名字",
    renameHint: "在 Memax 里怎么叫它。收据里叫它 {label}。",
    renameError: "名字要在 1 到 80 个字符之间。",
    renameDo: "重命名",
    cancel: "取消",
    stop: "停止写入这台电脑",
    stopLabel: "停止写入 {machine}",
    confirm:
      "{machine} 下次上线时，Memax 会把个人空间从上面撤下来。你手动改过的文件会原样留着。",
    confirmDo: "停止写入 {machine}",
    unlinked: "{date} 解除关联",
    unlinkedDetail: "在它上面运行 {command}，就能重新把个人空间写过去。",
  },
  toast: {
    renamed: "已把 {from} 改名为 {to}",
    renameClash:
      "{machine} 已经在别处被改了名。这是它现在的名字，还想改的话再改一次。",
    renameFailed: "{machine} 没改成名。{reason}",
    stopped: "已停止写入 {machine}。它下次上线时，Memax 会把文件撤下来。",
    stopFailed: "Memax 还在往 {machine} 写个人空间。{reason}",
    stoppedFile:
      "{machine} 上的 {file} 不再编译了。它下次上线时，Memax 会把这个文件撤下来。",
    stopFileFailed: "Memax 还在往 {machine} 写 {file}。{reason}",
  },
  target: {
    switcher: "电脑",
    switcherItem: "{machine}，{state}",
    on: "在 {machine} 上",
    lede: {
      claude_user:
        "{machine} 上的 Claude Code 在每个项目、每次会话开始时都会读它。每次保留和忘记之后，Memax 都会从你的个人空间重新写它。",
      codex_user:
        "{machine} 上 {file} 里属于 Memax 的那一段，Codex 在每个项目里都会读。这个文件里你自己写的内容只留在你的电脑上，Memax 不读。",
      gemini_user:
        "{machine} 上 {file} 里属于 Memax 的那一段，Antigravity 在每个项目里都会读；Gemini CLI 设置成读 AGENTS.md 之后也会读。这个文件里你自己写的内容只留在你的电脑上，Memax 不读。",
      agents_md:
        "{machine} 上把整个个人空间写成的一个文件，给你自己看，也给没有专属文件的 Agent 读。",
    },
    fileHead: "{machine} · {file}",
    block:
      "这里只显示 Memax 的那一段。这个文件里你自己写的内容留在 {machine} 上，Memax 不读。",
    delivery: "由 {machine} 上的 memax 后台程序写入。",
    budgetHint:
      "每个 Agent 每次会话都会读它，所以要短。其余的可以通过 MCP 实时读取。",
    budgetError: "要在 1 到 16 KB 之间。",
    stop: "停止写入这台电脑",
    off: "Memax 已停止在 {machine} 上编译这个文件。那台电脑上的 Agent 仍可以通过 MCP 读取你的个人空间。",
    unlinked:
      "{machine} 已解除关联，Memax 不再往上面写东西。在它上面运行 {command}，就能重新把个人空间写过去。",
  },
  drift: {
    eyebrow: "简报 · 编译到 · {machine} 上的 {file}",
    title: "{machine} 上的 {file} 被手动改过",
    lede: "有人{when}在 {machine} 上绕过 Memax 改了它。没有你的同意，Memax 不会覆盖它。",
    observed: "{machine} 上现在的样子",
    stopDetail:
      "{tool} 仍可以通过 MCP 实时读取你的个人空间，{machine} 上改过的文件原样保留。",
    notice: "{machine} 上 {agent} 的文件有 1 处本地改动。",
    noticeMany: "{machine} 上 {agent} 的文件有 {n} 处本地改动。",
    noticeFile: "{machine} 上的 {file} 有 1 处本地改动。",
    noticeFileMany: "{machine} 上的 {file} 有 {n} 处本地改动。",
    noticeBody: "有人{when}在 {machine} 上手动改了 {file}。",
  },
  reach: {
    keep: "保留之后，你关联的每台电脑上的每个 Agent 都会读到它。",
    write: "在个人空间里，它保留的内容会传到你关联的每台电脑上的每个 Agent。",
    writeNow:
      "{agent} 现在可以直接写入 {space}，照样带收据。它保留的内容会传到你关联的每台电脑上的每个 Agent。",
  },
  session: {
    writes: "在写个人空间",
    signedOut:
      "{client} 已退出登录。它的后台程序下次运行时，会把个人空间从 {machine} 上撤下来。",
  },
  setup: {
    written: "个人空间：已在 {machine} 上为 {agents} 写好。",
    writtenNone: "个人空间：已写到 {machine} 上。",
    notYet: "个人空间还没写进你的 Agent 自己的文件。在你用的每台电脑上运行：",
    reads: "在你关联的每台电脑上，从 {file} 读取你的个人空间。",
    readsHint: "还没关联的电脑，先运行 {command}。",
  },
};
