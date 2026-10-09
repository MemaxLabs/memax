// 设置 › 通知（Notifications.png）和设置 › 安全的中文文案，挂在
// `t.ledger.settings` 下，键与 ./settings-en.ts 一一对应。
import type { Translations } from "../en";

export const ledgerSettingsZh: Translations["ledger"]["settings"] = {
  notifications: {
    title: "通知",
    lede: "Memax 只在有事等你处理时打扰你。其余的都在「今天」里等着。",
    label: "每件事怎么通知你",
    columns: {
      when: "什么时候",
      inApp: "应用内",
      email: "邮件",
      phone: "手机",
      slack: "Slack",
    },
    cell: "{event}：{channel}",
    inAppOn: "应用内，始终开启",
    notYet: "暂时还不能用",
    events: {
      decision_gate: {
        title: "Agent 请你做决定",
        meta: "决策关口，以及交接里的问题",
        short: "决策关口",
      },
      morning_edition: {
        title: "晨报",
        meta: "Dream 夜里改了什么，{time} 送到",
        metaReady: "Dream 夜里改了什么，做完就送到",
        short: "晨报",
      },
      review_waiting: {
        title: "提议等了一天",
        titleDays: "提议等了 {n} 天",
        meta: "一天一次，不会每条提议都发",
        short: "等待中的提议",
      },
      drift: {
        title: "编译出的文件漂移了",
        meta: "有人改了 Memax 写的文件",
        short: "文件漂移",
      },
      stale: {
        title: "你保留的内容过时了",
        meta: "它的来源变了",
        short: "过时的记忆",
      },
      write_held: {
        title: "有写入被拒绝或被扣下",
        meta: "没有收据，或内容来自网页",
        short: "被拒绝或被扣下的写入",
      },
      weekly_summary: {
        title: "每周摘要",
        meta: "每周一：保留、拒绝、忘记、读取",
        short: "每周摘要",
      },
      forget_done: {
        title: "一次忘记完成了",
        meta: "每个文件都已重写，每个 Agent 都已告知",
        short: "完成的忘记",
      },
      agent_changed: {
        title: "Agent 的连接变了",
        meta: "接入、暂停、断开，或能做的事多了或少了",
        short: "Agent 连接",
      },
    },
    sentNow:
      "Memax 现在会发邮件的只有{list}。其他选择会先记着，等相应的邮件上线后照办。",
    sentNone: "这台服务器还不发邮件。你的选择会先记着，等它发的时候照办。",
    channelsLater: "手机和 Slack 暂时还不能用。",
    quiet: {
      title: "免打扰时段",
      zone: "按 {zone}",
      zoneDefault: "按 UTC，直到 Memax 得知你的时区",
      changeZone: "更改时区",
      from: "从",
      until: "到",
      gates: "决策关口仍然通知我",
      off: "已关闭。设好两个时间，夜里的邮件就会等到早上。",
      invalid: "请用 24 小时制时间，比如 08:00",
      same: "结束时间要和开始时间不同",
    },
    clash:
      "你的设置在别处改过了，可能是通过退订链接。已经重新载入，请再改一次。",
    failed: "没有成功。检查一下网络，再试一次。",
    loading: "正在载入你的通知设置",
    loadFailed: "你的通知设置没有载入。",
    retry: "再试一次",
  },
  security: {
    title: "安全",
    lede: "Memax 为你保留了什么、放在哪里，以及它够不到的地方。这一页读的是服务器自己的设置。",
    loading: "正在读取这台服务器的说明",
    loadFailed: "安全信息没有载入。",
    retry: "再试一次",
    receipts: {
      title: "收据",
      meta: "每个空间一条链",
      label: "每个空间的封存",
      signedWith: "用密钥 {key} 签名。",
      nothing: "还没有封存任何收据。",
      noSpaces: "你的空间都还没切到 V2 记录，所以没有要封存的收据。",
      loading: "正在读取封存",
    },
    export: {
      title: "导出与核验",
      body: "把一个空间的完整记录连同收据和签名检查点一起带走，在你自己的机器上核验，不必信任 Memax。",
      note: "verify-export 会核对每个文件，从第一条收据起重算整条链，并检查每个签名。它信任你用 --key 给的密钥，否则用服务器公布的密钥。",
    },
    forget: {
      title: "忘记能够到哪里",
      reaches: "能够到",
      out: "Memax 够不到的地方",
      reach: {
        words:
          "Memax 里这些文字的每一份：每个版本、它的来源、收据里的理由、搜索条目，以及 Memax 的核对记录",
        files: "每个编译出的文件，一分钟内重写",
        agents: "每个读过它、或接入了这个空间的 Agent，在它下一次读取时告知",
        tombstone: "会留下一块墓碑，让你看到这件事发生过。墓碑里没有文字。",
      },
      unreachable: {
        backups:
          "数据库备份，保留 {days} 天。任何一次恢复之后，Memax 都会把每次忘记重新执行一遍。",
        backupsOne:
          "数据库备份，保留 1 天。任何一次恢复之后，Memax 都会把每次忘记重新执行一遍。",
        git: "编译出的文件在你仓库历史里的旧提交",
        agentMemory: "Agent 存进它们自己记忆里的内容",
        handEdits: "有手动修改、Memax 不会覆盖的文件",
        copies: "粘贴到别处的副本，比如 ChatGPT 项目",
        providers: "{names} 按它们的条款留下的、发给它们的内容（见下）",
      },
    },
    residency: {
      title: "你的数据在哪里",
      what: "什么",
      where: "在哪里",
      holds: {
        database: "记忆、收据、来源和你的设置",
        compute: "API 和后台任务，它们处理这些数据",
        objects: "收据检查点、编译出的文件和忘记记录",
        edge: "网页应用和编译服务，请求之间什么都不留",
      },
      at: "{provider}，{region}",
      encryption:
        "传输中加密，存储时按各服务商的默认方式加密。暂不提供按空间分开的密钥。",
    },
    processors: {
      title: "还有谁会看到记忆里的文字",
      meta: "只在对应功能开启时",
      service: "服务",
      gets: "收到什么",
      keeps: "留下什么",
      uses: {
        judge: "核对的第一轮",
        judge_fallback: "核对的备用模型",
        judge_strong: "核对里对生效中决策的复核",
        ask: "提问的回答",
        dream: "Dream",
        dream_fallback: "Dream 的备用模型",
        dream_strong: "Dream 里对生效中决策的复核",
        embeddings: "你保留和提议的内容的向量",
        queries: "搜索的向量",
        rerank: "搜索结果重排",
        email: "发给你的晨报邮件",
      },
      model: "{use}：{model}",
      hosts: "运行在 {hosts}",
      precision: "{precision} 或更高精度",
      retention: {
        zero: "什么都不留。每次调用都只走零数据保留的端点。",
        unconfirmed:
          "尚未确认。除非账户选择退出，{name} 会保留 API 输入用于训练，而 Memax 还没有确认已经退出。发给它的内容请当作按它的条款保留。",
        provider_terms: "按 {name} 的条款。Memax 无法控制。",
      },
      none: "这台服务器不会把记忆里的文字发给外部服务。",
    },
    agents: {
      title: "Agent 与自主程度",
      summary: "{agents} 接入了 {spaces}。",
      agents: "{n} 个 Agent",
      agentsOne: "1 个 Agent",
      spaces: "{n} 个空间",
      spacesOne: "1 个空间",
      levels: "最多能做：{list}。",
      level: {
        read: "{n} 个读取",
        propose: "{n} 个提议",
        write: "{n} 个写入",
      },
      paused: "{n} 个已暂停。",
      none: "还没有 Agent 接入你的空间。",
      rule: "Agent 提议，人来保留。设为“写入”的 Agent 可以保留自己写的内容，每一条都有收据；只有你本人在网页上，才能让一个 Agent 做更多。",
      link: "Agent 和密钥",
    },
    assurance: {
      title: "你的保留算作什么",
      body: "只有当你在 Memax 网页应用里登录、并由它的服务器为请求签名时，保留才算作 human_web。从 CLI、Agent 或其他客户端保留，算作 client_attested，因为 Agent 可以用你的登录去操作它们。",
      needs: "在团队空间里保留决策，或保留来自外部的内容，需要 human_web。",
      session: "这次会话",
      verifiedLabel: "使用通行密钥时",
      verified:
        "你已添加通行密钥，所以这些保留、提高 Agent 的权限和遗忘都会要求用它确认，并算作 human_web_verified：被复制的浏览器会话或操控你浏览器的 Agent 都做不到。",
      nudge:
        "在“账户”里添加通行密钥后，这些保留会要求用它确认，并算作 human_web_verified，被复制的浏览器会话无法做到。",
      account: "账户",
    },
  },
};
