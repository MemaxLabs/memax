// 公开站点（计划 25 史诗 3.4）：首页、价格、安全（信任页）和文档。挂在
// `t.ledger.public` 下，键与 ./public-en.ts 一一对应。
//
// 这里的每句话都要对今天的产品成立：还没做好的写明“即将推出”，安全页的
// 事实来自服务器自己的默认配置（lib/v2/cloud-posture.json）。公司和模型
// 名称在 settings-copy.ts 的 NAMES 里。init 的终端输出是 CLI 实际打印的
// 内容，而 CLI 只打印英文，所以那几行在中文里保持原样。
import type { ledgerPublicEn } from "./public-en";

export const ledgerPublicZh: typeof ledgerPublicEn = {
  meta: {
    landing: {
      title: "Memax：属于你的上下文层",
      description:
        "Memax 把你的 Agent 学到的东西记成一份持续更新、带引用的记录，并编译进 Claude Code、Codex、Cursor 和 ChatGPT。每一条保留，都有收据。",
    },
    pricing: {
      title: "价格",
      description:
        "一个项目免费。每个方案都给每一行配上收据，都能把一切导出为 Markdown，忘记也都是真的忘记。",
    },
    security: {
      title: "信任与安全",
      description:
        "Memax 保留什么、放在哪里、还有谁会看到记忆里的文字，以及忘记如何放手。",
    },
    docs: {
      title: "快速开始",
      description:
        "从你的 Agent 已有的文件出发，五分钟得到一份它们都会读的 Brief。",
    },
    imageAlt: "Memax：一份上下文，每个 Agent",
  },
  site: {
    skip: "跳到正文",
    home: "Memax 首页",
    nav: {
      label: "站点",
      product: "产品",
      docs: "文档",
      pricing: "价格",
      trust: "信任",
      changelog: "更新日志",
    },
    signIn: "登录",
    getStarted: "开始使用",
    footer: {
      label: "页脚",
      docs: "文档",
      changelog: "更新日志",
      github: "GitHub",
      privacy: "隐私",
    },
  },
  landing: {
    eyebrow: "Memax 2 · 属于你的上下文层",
    titleLine1: "一份上下文。",
    titleLine2: "每个 Agent。",
    lede: "Memax 把你的 Agent 学到的东西记成一份持续更新、带引用的记录，并编译进 Claude Code、Codex、Cursor 和 ChatGPT。每一条保留，都有收据。",
    command: "启动 Memax 的命令",
    copy: "复制命令",
    copied: "已复制",
    copyFailed: "没能复制。请选中命令，自己复制。",
    howItWorks: "如何运作",
    worksWith:
      "一个项目免费。支持 Claude Code、Codex、Cursor、ChatGPT、Gemini CLI、Copilot 和 OpenCode。",
    example: {
      label: "一个示例空间",
      counts: "已保留 {kept} · 等你 {waiting}",
      river: "后台任务跑在 River 上，不用 Temporal。",
      errors: "API 错误统一用 RFC 9457 problem+json。",
      confirm: "MCP 写入工具必须用 input_required 请求确认。",
      keptDate: "10月5日",
      oct2: "10月2日",
      sep12: "9月12日",
      hourAgo: "1 小时前",
    },
    dayOne: {
      eyebrow: "第一天",
      title: "你的 Agent 早就各说各话。一次理清。",
      body: "一条命令读遍仓库里的每个 CLAUDE.md、AGENTS.md、Cursor 规则和 Agent 记忆。它合并重复的内容，指出彼此矛盾的地方，再把一份 Brief 写回每个工具。",
      personal:
        "你的 Agent 在这台电脑上了解到的关于你的事，会进入你的个人空间，跟着你去每一台你连接的电脑。",
      stats: {
        label: "这次运行找到的内容",
        files: "个文件",
        conflicts: "处矛盾待处理",
        brief: "份 Brief，每个工具",
      },
      transcript: {
        title: "~/code/memax · zsh",
        command: "npx memax-cli init",
        reading: "Reading what your agents already know",
        claude: "  CLAUDE.md                42 lines  →  18",
        agents: "  AGENTS.md                27 lines  →   9",
        cursor: "  .cursor/rules/*.mdc       3 files  →   6",
        codex: "  ~/.codex/memories         9 notes  →   7",
        folded: "  7 duplicates folded · 2 secrets skipped",
        testCommand: "Test command: pnpm test ≠ npm run test ≠ pnpm vitest run",
        packageManager: "Package manager: pnpm ≠ npm in /tools",
        deploy: "API deploy: Fly.io ≠ Railway",
        summary:
          "6 files, 3 conflicts, 1 Brief. Nothing kept until you say so.",
        personal: "Personal: 37 things your agents learned on this laptop.",
      },
    },
    how: {
      eyebrow: "如何运作",
      title: "保留一次，处处原生可读。",
      compiled: "编译进每个工具自己的文件",
      live: "通过 MCP 实时读取",
      inSync: "已同步",
      overConnector: "通过连接器实时读取",
      projectInstructions: "项目指令",
      readsAndProposes: "读取并提议",
    },
    principles: {
      label: "原则",
      write: {
        title: "写一次，每个 Agent 都知道。",
        body: "在一个工具里做的决定，会以下一个工具自己的格式出现在那里。周一不用再把技术栈解释一遍。",
      },
      review: {
        title: "先审阅，再记住。",
        body: "Agent 提议，你来保留。每条记忆都写明是谁、在什么时候、根据什么写的；来自外部网页的内容会等你决定。",
      },
      forget: {
        title: "真的忘记。",
        body: "忘记会重写每个编译出的文件，并告知每个 Agent。涂黑的痕迹会留下，让你看到这件事发生过。",
      },
    },
    cta: {
      title: "从你已有的文件开始。",
      start: "免费开始",
      pricing: "查看价格",
    },
  },
  pricing: {
    eyebrow: "价格",
    titleLine1: "一个项目免费。",
    titleLine2: "规模变大，价格公道。",
    lede: "每个方案都给每一行配上收据，都能把一切导出为 Markdown，忘记也都是真的忘记。当 Memax 覆盖更多项目、你的云端 Agent 或一个团队时，你才需要付费。",
    alpha: "Memax 在内测期间免费，目前还没有开放付费。下面是它将提供的方案。",
    period: {
      label: "计费周期",
      monthly: "按月",
      yearly: "按年",
    },
    plans: {
      free: { name: "Free", audience: "在一个项目上试用" },
      pro: { name: "Pro", audience: "同时用好几个 Agent 的开发者" },
      team: { name: "Team", audience: "3 到 30 人的团队" },
      enterprise: { name: "Enterprise", audience: "团队方案不够用时" },
    },
    price: {
      custom: "定制",
      month: "每月",
      year: "每年",
      seatMonth: "每席位每月",
      seatYear: "每席位每年",
      monthsFree: "按年付费，相当于每月 {price}：省下 {n} 个月",
      discount: "按年付费，相当于每席位每月 {price}：便宜 {n}%",
      seats: "能保留内容的成员算席位。查看者免费，没有最低席位数。",
    },
    features: {
      adapters: "编译进每个工具自己的文件",
      spaces: "{personal} 个个人空间和 {project} 个项目空间",
      agentKinds: "最多 {n} 个 Agent，按种类计",
      review: "审阅、收据和全部自主程度",
      dreamWeekly: "Dream，每周一次",
      askMonthly: "提问，每月 {n} 次",
      activityDays: "{n} 天的动态记录",
      unlimited: "不限空间和 Agent 数量",
      cloudAgents: "云端和后台 Agent",
      dreamNightly: "Dream，在你最常用的 {spaces} 个空间每晚运行",
      staleWatched: "盯着你仓库的过时检测",
      handoffsGates: "在你自己的 Agent 之间交接和请你决定",
      viewers: "每个空间 {n} 位免费查看者",
      everythingInPro: "包含 Pro 的全部内容",
      teamSpaces: "团队空间和角色",
      decisionLedger: "决策账本，可导出 ADR",
      routing: "把决定请求和交接转给队友",
      integrations: "Slack、Linear 和 webhook",
      auditYear: "一年的审计记录",
      sso: "SSO 和 SCIM",
      euResidency: "数据存放在欧盟",
      selfHosting: "有支持的自托管",
    },
    coming: "即将推出",
    actions: {
      sign_in: "免费开始",
      coming: "即将推出",
      contact: "联系我们",
    },
    closed: {
      pro: "目前还没有开放付费。",
      team: "团队空间和付费都还没有开放。",
    },
    every: {
      label: "每个方案都有",
      receipts: {
        title: "每一行都有收据",
        body: "是谁、在什么时候、根据什么写的，每个方案都有。",
      },
      export: {
        title: "一切都能导出为 Markdown",
        body: "收据和来源都在 frontmatter 里。想走随时可以走。",
      },
      forget: {
        title: "忘记能到达 Memax 持有的每一份副本",
        body: "每个编译出的文件都会重写，每个 Agent 都会被告知。",
      },
    },
    faq: {
      title: "常见问题",
      replace: {
        q: "Memax 会取代 Claude 或 ChatGPT 自带的记忆吗？",
        a: "不会。它通过它们自己的文件和连接器为它们提供内容。自带的记忆继续用于聊天；Memax 存放每个 Agent 都应该知道的东西。",
      },
      agent: {
        q: "什么算一个 Agent？",
        a: "任何接入空间的工具，按种类计：Claude Code、Codex、Cursor、ChatGPT、Gemini CLI、Copilot、OpenCode 或其他任何 MCP 客户端。同一个 Agent 用在两台电脑上只算一次；只读取编译出的文件的工具不算。",
      },
      store: {
        q: "Memax 存了什么？",
        a: "你保留的陈述和等你决定的提议，连同它们的收据和指向来源的链接。密钥在存储之前就会被拒绝。",
      },
      leave: {
        q: "我可以离开吗？",
        a: "可以。随时都能把一切连同收据导出为 Markdown，每个方案都可以，还能在你自己的电脑上核验导出的内容。",
      },
    },
  },
  security: {
    eyebrow: "信任",
    titleLine1: "Memax 保留什么，",
    titleLine2: "又如何放手。",
    lede: "Agent 能写入的记忆，攻击者也能写入。下面是 Memax 遵守的规则，每个方案都一样。",
    rules: {
      label: "规则",
      receipts: {
        title: "每次写入都有收据。",
        body: "写明 Agent、会话、时间和来源，否则写入会被拒绝。Agent 读到的每一行，都能追溯到是谁写的。",
        where: "在产品里：每条记忆右侧的收据栏",
      },
      propose: {
        title: "Agent 提议，人来保留。",
        body: "每个 Agent 是只读、提议还是直接写入，由你决定。Agent 提议的内容，在有人保留之前，不会传到别的 Agent 那里。",
        where: "在产品里：Agent 页和审阅",
      },
      outside: {
        title: "来自外部的内容要等待。",
        body: "Agent 在网页、邮件或 issue 里读到的任何内容，不管这个 Agent 平时能做什么，进来时都会被隔离。这是对植入记忆的防线，OWASP 面向 Agent 应用的十大风险把它称作记忆与上下文投毒（ASI06）。",
        where: "在产品里：审阅里的隔离提示",
      },
      forget: {
        title: "忘记能到达 Memax 持有的每一份副本。",
        body: "忘记会在一分钟内重写每个编译出的文件，并在每个 Agent 下一次读取时告知它。会留下一块墓碑，让你看到这件事发生过。",
        where: "在产品里：被忘记的记忆的页面",
      },
      secrets: {
        title: "密钥绝不落地。",
        body: "导入和写入都会被扫描，密钥和令牌在存储之前就会被拒绝。第一天的整理会告诉你跳过了什么。",
        where: "在产品里：“跳过了 2 个密钥”",
      },
      leave: {
        title: "一切都能随你带走。",
        body: "随时都能把一个空间的完整记录导出为 Markdown，每条记忆的收据和来源都在 frontmatter 里，每个方案都可以。",
        where: "在产品里：记忆 › 导出为 Markdown，以及 memax export",
      },
    },
    data: {
      title: "你的数据",
      what: "什么",
      where: "在哪里",
      howLong: "保留多久",
      cloud: "Memax 云端：{place}",
      memories: {
        what: "记忆、收据和指向来源的链接",
        howLong: "直到你忘记它们。收据会留下，但不含文字。",
      },
      notes: {
        what: "Agent 写下的原始笔记",
        where: "Memax 云端",
        howLong: "直到你忘记它们。Dream 会把它们整理成提议，交给你审阅。",
      },
      compiled: {
        what: "编译出的文件",
        where: "你的仓库或电脑上，每次编译的副本在 {place}",
        howLong: "它们属于你。忘记会重写它们，以及 Memax 的副本。",
      },
      reads: {
        what: "Agent 的读取",
        where: "Memax 云端",
        howLong: "13 个月，只记次数。读取记录里从不包含记忆或查询的文字。",
      },
      code: {
        what: "你的代码",
        where: "不存储",
        howLong: "Memax 只把文件路径和提交记为来源，从不存你的源代码。",
      },
      backups: {
        what: "数据库备份",
        howLong:
          "{days} 天。任何一次恢复之后，Memax 都会把每次忘记重新执行一遍。",
        howLongOne:
          "1 天。任何一次恢复之后，Memax 都会把每次忘记重新执行一遍。",
      },
      runs: "Memax 运行在哪里",
      certifications: "Memax 目前还没有任何第三方认证，例如 SOC 2。",
    },
    processors: {
      lede: "只有这些服务，而且只在它们服务的功能开启时。每次模型调用都经过 OpenRouter 的零数据保留路由，固定到下面列出的主机，精度不低于 fp8，从不调用 DeepSeek 自己的 API。",
      source:
        "这些是服务器的默认配置，Memax 云端就按它们运行。登录后，设置 › 安全会从正在运行的服务器读取它们。",
      original:
        "仍在原有记录上的空间走它自己的处理流程：调用 OpenRouter 时不经过这套路由，也可能用 Cohere 重排。把空间切换到 V2 记录，就会改走上面的路由。",
    },
    forget: {
      lede: "忘记会把文字从 Memax 持有的一切里拿走。有些副本它够不到，每块墓碑都会写明。",
    },
    check: {
      title: "自己核验",
      receipts: {
        title: "封存的收据",
        body: "每个空间的收据组成一条哈希链。每隔 10 到 60 秒，Memax 会把最新的收据封存进一个用 Ed25519 签名的检查点；每天夜里，它会从第一条收据起重算每一条链。",
      },
      export: {
        title: "导出与核验",
        body: "把一个空间的完整记录连同收据和签名检查点一起带走，在你自己的电脑上核验，不必信任 Memax。",
      },
      passkeys: {
        title: "通行密钥",
        body: "添加通行密钥之后，需要由人来做的决定都会再向你要一次：保留来自外部的内容、让 Agent 做更多事，以及每一次忘记。被复制的浏览器会话无法做出这些决定。",
      },
      sessions: {
        title: "会话",
        body: "刷新令牌只以哈希形式存储，每用一次就换一个。旧令牌再次被使用，它所在的会话就会被登出。",
      },
    },
    report: {
      title: "发现了问题？",
      body: "请写信到 {email}。我们会在两个工作日内回复。",
      docs: "了解 Memax 如何运作",
    },
  },
  docs: {
    header: {
      label: "文档",
      search: "搜索文档",
      matches: "{n} 个页面匹配",
      matchesOne: "1 个页面匹配",
      noMatch: "没有匹配的页面。完整文档在 {site}。",
      open: "打开 Memax",
    },
    tree: {
      label: "文档",
      start: "开始",
      agents: "Agent",
      concepts: "概念",
      reference: "参考",
      quickstart: "快速开始",
      install: "安装 CLI",
      personal: "你的个人空间",
      claudeCode: "Claude Code",
      plugin: "Claude Code 插件",
      codex: "Codex",
      cursor: "Cursor",
      cloud: "云端 Agent",
      otherAgents: "任何 MCP 客户端",
      memories: "记忆",
      dream: "Dream",
      security: "安全",
      exportFormat: "导出格式",
      cli: "CLI",
      init: "memax init",
      compile: "memax compile",
      connect: "memax connect",
      mcp: "MCP 服务器",
    },
    eyebrow: "开始",
    title: "快速开始",
    lede: "从你的 Agent 已有的文件出发，五分钟得到一份它们都会读的 Brief。",
    init: {
      title: "1. 读取你的 Agent 已经知道的东西",
      body: "在仓库根目录运行这条命令。它会找到你的 Agent 和它们的文件，把所有内容作为提议导入。现在还什么都没保留。",
    },
    settle: {
      title: "2. 处理矛盾，然后审阅",
      body: "两个文件说法不一时，init 会在终端里问你一次。然后保留属实的内容：可以在那里一次全部保留，也可以在网页的审阅里逐条决定。",
    },
    compile: {
      title: "3. 编译进每个工具自己的文件",
      body: "Memax 根据你保留的内容写出 AGENTS.md、引用它的 CLAUDE.md，以及你开启的其他文件。每次改动后它都会重新编译，memax compile 会让它立刻编译一次。把这些文件提交进仓库，每个克隆和云端 Agent 也都能读到。",
    },
    cloud: {
      title: "4. 接入云端 Agent",
      body: "在你电脑之外运行的 Agent，通过 MCP 实时读取同一份上下文。它们用 OAuth 登录，并请你选择使用哪个空间。",
      start:
        "Claude Code 的云端会话和 Copilot 的云端 Agent 还可以从它们载入的文件之后的变化开始：memax connect --cloud 会添加一个启动密钥和一个钩子。",
    },
    tools: {
      title: "MCP 工具",
      tool: "工具",
      does: "做什么",
      memax_recall:
        "这个 Agent 该知道的：针对一个查询的已保留记忆，没有查询时给出每个空间的摘要",
      memax_search: "搜索已保留的记忆和决策，需要时也搜索笔记",
      memax_get: "一条记忆，连同它的收据和来源",
      memax_list: "已保留的记忆，从新到旧，一次一页",
      memax_push: "提议一条记忆。由人来保留，在 Agent 里或在审阅里",
      memax_capture: "会话笔记，Dream 会把它们整理成提议",
      memax_request_decision: "请一个人做决定，并拿回答案",
      memax_forget: "请求忘记一条记忆。由人来确认。",
      memax_hubs: "这个 Agent 能读取的空间，以及它在每个空间的自主程度",
      memax_hub_members: "一个空间的成员和他们的角色",
      memax_topics: "一个空间的各个部分和其中已保留的记忆",
    },
    note: "除非你把 Agent 设为直接写入，它写的内容都是提议。它在网页上读到的内容永远是提议。",
    next: "下一页",
    toc: {
      label: "本页内容",
      init: "读取 Agent 已知的内容",
      settle: "处理与审阅",
      compile: "编译",
      cloud: "云端 Agent",
      tools: "MCP 工具",
    },
  },
};
