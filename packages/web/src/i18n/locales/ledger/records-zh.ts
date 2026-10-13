// 审阅、记忆列表和记忆详情共用的文案，挂在 `t.ledger.records` 下，
// 键与 ./records-en.ts 一一对应。术语沿用 Ledger：保留、拒绝、编辑、
// 忘记、审阅、交接；"Agent" 和 Agent 名称不翻译。
import type { Translations } from "../en";

export const ledgerRecordsZh: Translations["ledger"]["records"] = {
  verbs: {
    proposed: "提议",
    kept: "保留",
    edited: "编辑",
    rejected: "拒绝",
    merged: "合并",
    flagged: "标记",
    resolved: "解决",
    verified: "核对",
    faded: "淡出",
    restored: "恢复",
    forgot: "忘记",
    moved: "移动",
    compiled: "编译",
    handed_off: "交接",
    answered: "回答",
    undid: "撤销",
    returned: "退回审阅",
    unflagged: "不再过时",
    asked: "提问",
    withdrawn: "撤回",
    updated: "更新",
    editing: "编辑中",
  },
  actor: {
    you: "你",
    teammate: "一位队友",
    dream: "Dream",
    memax: "Memax",
    repository: "仓库",
  },
  time: {
    minutesAgo: "{n} 分钟前",
    hoursAgo: "{n} 小时前",
    justNow: "刚刚",
    dateTime: "{date} {time}",
  },
  expires: {
    minutes: "{n} 分钟后过期",
    today: "今天 {time} 过期",
    tomorrow: "明天 {time} 过期",
    date: "{date} 过期",
  },
  sessionRef: "会话 {session}",
  sections: {
    decisions: "决策",
    conventions: "约定",
    preferences: "偏好",
    open_question: "待定问题",
  },
  sourceKinds: {
    session: "会话",
    pr: "拉取请求",
    file: "仓库",
    url: "网页",
    issue: "Issue",
    email: "邮件",
    note: "笔记",
    import: "导入",
  },
  notes: {
    conflict: "和 {ref} 冲突，那条是 {name} 保留的。",
    conflictAsked: "{agent} 在等你拿主意。",
    stale: "它的来源在 {date} 变了",
    staleSource: "它的来源在 {date} 变了 · {source}",
    staleDate: "到了该再核对一次的日子",
    staleCheck: "它的一个前提在 {date} 不再成立",
    merged: "已由 {name} 合并进 {ref}",
    faded: "已经 {n} 天没有 Agent 读过它了。可以恢复，也可以就这样放着。",
  },
  editor: {
    statement: "内容",
    yourChangeTo: "你对 {name} 提议的改动",
    yourChange: "你的改动",
    why: "为什么改",
    whyHint: "会记在收据里，Agent 和队友都能看到你的理由。",
    editedFrom: "改自 {name} 的提议",
    cancel: "取消",
    keepEdited: "保留改后的",
    unchanged: "先改一下内容，或者按 Esc 不改了。",
    empty: "记忆总得有内容。写点什么，或者按 Esc 不改了。",
  },
  clash: {
    region: "编辑冲突",
    by: "{name} {ago}保留了对这条的改动。",
    byYou: "你在另一个窗口里，{ago}保留了对这条的改动。",
    byTeammate: "有人 {ago}保留了对这条的改动。",
    yours: "你的：“{text}”",
    keepTheirs: "用对方的",
    combine: "合在一起",
    keepMine: "用我的",
  },
  toast: {
    rejected: "已拒绝 {ref}",
    edited: "已编辑 {ref}",
    editedRecompiled: "已编辑 {ref} · 重新编译了 {n} 个文件",
    editedRecompiledOne: "已编辑 {ref} · 重新编译了 1 个文件",
    copied: "已复制 [{ref}] 和链接",
    copyFailed: "没复制上。选中编号自己复制一下吧。",
  },
  failure: {
    keep: "{ref} 没保留上。",
    reject: "{ref} 没拒绝成。",
    edit: "你对 {ref} 的改动没保留上。",
    resolve: "{ref} 没裁定成。",
    answer: "{ref} 没回答上。",
    withdraw: "{ref} 没撤回成。",
    forget: "{ref} 没忘记成。",
    verify: "{ref} 没核对成。",
    declineForget: "{ref} 没保留上。",
    scope: "{ref} 的适用范围没有改。",
    carriesChanged:
      "你确认之后，跟它一起被忘记的内容变了，所以什么都没忘记。再看一遍，然后再忘记它。",
    ended: {
      answered: "它已经有人回答了。",
      withdrawn: "{agent} 把这个问题撤回了。",
      expired: "它已经过期，{agent} 不再等了。",
    },
    unreachable: "没连上 Memax，所以什么都没变。再试一次吧。",
    passkeyDeclined: "这需要你的通行密钥，所以没有任何改变。",
    passkeyExpired: "通行密钥确认用时太长，所以没有任何改变。请重试。",
    passkeyNotVerified:
      "你的通行密钥没有验证你的身份（指纹、面容或 PIN）。请解锁后重试。",
    passkeySignIn: "这个会话已经退出，请重新登录。",
    passkeyInvalid: "你的通行密钥回答没有通过验证，所以没有任何改变。请重试。",
    busy: "另一个改动正占着它。稍等一下再试。",
    busyJudge: "Memax 还在对照现行的决策检查它。稍等一下再试。",
    inConflict: "它和现行的决策 {with} 矛盾。对比两边，定下来。",
    inConflictBare: "它和一条现行的决策矛盾。对比两边，定下来。",
    rateLimited: "一下子太多了。等 {n} 秒再试。",
    rateLimitedSoon: "一下子太多了。稍等一下再试。",
    decided: "它已经处理过了，所以从队列里拿掉了。",
    notFound: "它已经不在这儿了，可能被忘记或者移走了。",
    clash: "你打开之后它又变了。屏幕上是最新的版本。",
    unavailable: "这个暂时还不能用。",
    unknown: "再试一次；要是一直这样，刷新一下页面。",
    retry: "再试一次",
    createSpace: "{name} 没建成。",
    refusedSpace: {
      personal_exists: "你已经有个人空间了，每个人只有一个。",
      space_kind: "团队空间随 Team 推出。现在先建一个项目空间。",
      space_limit: "你已经有 50 个项目空间，这是一个人能拥有的上限。",
      space_by_person: "只有本人登录后才能建空间，Agent 的密钥不行。",
      other: "Memax 拒绝了：{message}",
      otherBare: "Memax 拒绝了。",
    },
    planLimit: {
      space_limit: "{plan} 套餐的 {allowed} 个项目空间你都在用了。",
      space_limitOne: "{plan} 套餐的项目空间你已经在用了。",
      agent_kind_limit:
        "{plan} 套餐可以连接 {allowed} 种 Agent，你已经连满了。",
      agent_kind_limitOne: "{plan} 套餐可以连接 1 种 Agent，你已经连上了。",
      ask_limit:
        "{plan} 套餐本月的 {allowed} 次提问你都用完了，下月 1 日重新计数。",
      seat_needed:
        "在 {plan} 套餐上，{space} 里只有所有者能保留。成员需要 Team 席位才能保留。",
      dream_cadence:
        "{plan} 套餐本周允许的立即运行次数已经用完。Dream 仍会按计划自动运行。",
      dream_cadenceToday:
        "{plan} 套餐今天允许的立即运行次数已经用完。Dream 仍会按计划自动运行。",
      upgrade: {
        space_limit: "Pro 套餐不限项目空间数量。",
        agent_kind_limit:
          "Pro 套餐不限 Agent 种类；同一种 Agent 换一台电脑不算新的一种。",
        ask_limit: "Pro 套餐每月可以提问 1,000 次。",
        seat_needed: "在 Team 套餐上，每位成员都有席位。",
        dream_cadence: "Pro 套餐每晚运行 Dream。",
      },
    },
    plans: { free: "Free", pro: "Pro", team: "Team", enterprise: "Enterprise" },
    refused: {
      viewer: "查看者可以提议和评论，保留要由成员来。",
      seat_needed:
        "在这个套餐上，{space} 里只有所有者能保留。成员需要 Team 席位才能保留。",
      owners_keep: "在 {space} 里只有所有者能保留。请所有者来保留吧。",
      external_needs_review:
        "它引用了外部来源，只有在网页上登录才能保留。退出后在这个页面重新登录，再来保留。",
      decision_needs_web:
        "{space} 里的决策要在网页上登录才能保留。退出后在这个页面重新登录，再来保留。",
      person_must_review:
        "只有真人才能处理它。用你自己的账号登录，别用 Agent 的密钥。",
      key_cannot_review:
        "API 密钥只能提议，不能保留或拒绝。在网页上登录来审阅吧。",
      proposal_in_review:
        "它还在审阅里等着。去那儿保留或拒绝，或者提一条新的。",
      secret_detected:
        "这看起来像是密钥之类的凭据，Memax 从不存这种东西。去掉之后再试。",
      not_member: "你不是 {space} 的成员。请所有者邀请你。",
      unknown_actor: "Memax 认不出这次登录。重新登录一下吧。",
      undo_by_decider:
        "只有当初处理 {ref} 的人才能撤销。可以直接改它，或者去问问对方。",
      person_must_answer:
        "问题只能由真人回答。用你自己的账号登录，别用 Agent 的密钥。",
      not_your_gate:
        "只有提问的 Agent、它所属的人，或者能回答的人，才能撤回这个问题。",
      verify_by_person:
        "只有真人能核对它。用你自己的账号登录，别用 Agent 的密钥。",
      space_on_v1:
        "{space} 还在 V1 上，切换到 V2 之前这里什么都不会改动。所有者可以在它的今天页切换。",
      other: "Memax 没接受：{message}",
      otherBare: "Memax 没接受。",
    },
    refusedAnswer: {
      decision_needs_web:
        "{space} 里的决策只能在 memax.app 上回答，Memax 没法确认这次是从那里来的。在这里重新登录，再来回答。",
      viewer: "查看者可以看问题，回答要由成员来。",
      owners_keep: "在 {space} 里只有所有者能回答决策。请所有者来回答吧。",
      key_cannot_review: "API 密钥只能提问，不能回答。在网页上的审阅里回答吧。",
    },
    refusedForget: {
      forget_not_allowed: "在 {space} 里只有所有者能忘记。请所有者来忘记它吧。",
      viewer: "查看者不能忘记。请所有者来忘记它吧。",
      key_cannot_forget: "API 密钥不能忘记。用你自己的账号登录后再忘记它。",
      person_must_forget:
        "只有真人才能忘记。用你自己的账号登录，别用 Agent 的密钥。",
      decision_needs_web:
        "{space} 里的决策要在网页上登录才能忘记。退出后在这个页面重新登录，再忘记它。",
      secret_detected:
        "你的备注看起来像是密钥之类的凭据，Memax 从不存这种东西。从备注里去掉之后再试。",
    },
    needsWebDev: "本地开发时，给网页端和 API 都配上 WEB_SURFACE_SECRET。",
    signInAgain: "重新登录",
  },
  undo: {
    action: "撤销",
    done: {
      keep: "已撤销保留。{ref} 回到了审阅。",
      reject: "已撤销拒绝。{ref} 回到了审阅。",
      edit: "已撤销你的编辑。{ref} 恢复成原来的文字。",
      editKeep: "已撤销编辑和保留。{ref} 回到了审阅。",
      resolve: "已撤销这次裁定。{ref} 作为冲突回到了审阅。",
      fold: "已取消合并 {ref}。它回到了审阅。",
      verify: "已撤销核对。{ref} 又标成过时了。",
      verifyUpdate: "已撤销更新。{ref} 恢复了原来的说法，又标成过时了。",
      remember: "已撤销记住 {ref}。它已撤回。",
      scope: "已撤销这次更改。{ref} 恢复了原来的适用范围。",
    },
    refused: {
      window_passed:
        "{ref} 是 10 分钟之前处理的，已经不能撤销了。去它的页面上直接改吧。",
      window_passed_fold:
        "{ref} 是 14 天之前合并的，已经不能取消合并了。去它的页面上直接改吧。",
      already_undone: "这已经撤销过了。{ref} 还是原来的样子。",
      not_undoable: "对 {ref} 的这个改动不能撤销。去它的页面上直接改吧。",
      later_changes:
        "{blocker} 在这之后又改过，撤销会丢掉那次改动。先撤销那次，或者去 {ref} 的页面上直接改。",
      later_changes_self:
        "{ref} 在这之后又改过，撤销会丢掉那次改动。去它的页面上直接改吧。",
      later_brief: "简报现在引用了 {ref}。先把它从简报里拿掉，再来撤销。",
      undo_by_decider:
        "只有当初处理 {ref} 的人才能撤销。可以直接改它，或者去问问对方。",
    },
    unreachable: "撤销没连上 Memax，所以什么都没变。再试一次吧。",
    notFound: "{ref} 已经不在这儿了，没什么可撤销的。",
    other: "Memax 没接受这次撤销：{message}",
    unknown: "撤销没成功。再试一次；要是一直这样，刷新一下页面。",
  },
};
