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
    ended: {
      answered: "它已经有人回答了。",
      withdrawn: "{agent} 把这个问题撤回了。",
      expired: "它已经过期，{agent} 不再等了。",
    },
    unreachable: "没连上 Memax，所以什么都没变。再试一次吧。",
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
    refused: {
      viewer: "查看者可以提议和评论，保留要由成员来。",
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
