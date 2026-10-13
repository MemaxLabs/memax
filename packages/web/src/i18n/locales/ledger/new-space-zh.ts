// 新建空间（NewSpace.png）的中文文案，挂在 `t.ledger.newSpace` 下，键与
// ./new-space-en.ts 一一对应。
import type { Translations } from "../en";

export const ledgerNewSpaceZh: Translations["ledger"]["newSpace"] = {
  title: "新建空间",
  close: "关闭",
  kind: "类型",
  kindLabel: "空间类型",
  kinds: {
    personal: {
      title: "个人",
      detail: "只有你：你在每个工具里习惯的工作方式。",
    },
    project: {
      title: "项目",
      detail: "一个代码库，编译进它的仓库。",
    },
    team: {
      title: "团队",
      detail: "成员和他们的 Agent，带角色和决策。",
    },
  },
  personalExists: "你已经有一个了：{space}。每个人只有一个个人空间。",
  openPersonal: "打开 {space}",
  teamLater: "团队空间随 Team 推出。现在先建一个项目空间。",
  name: "名称",
  nameRequired: "给空间起个名字。",
  nameTooLong: "名称最多 80 个字符。",
  repository: "仓库",
  repositoryPlaceholder: "owner/name",
  repositoryInvalid: "写成 owner/name 的形式，例如 MemaxLabs/memax-web。",
  belongsTo: "归属",
  justMe: "只有我",
  teamOwnerLater: "属于团队的空间随 Team 推出。",
  startFrom: "从这里开始",
  start: {
    agents: {
      title: "这个仓库里 Agent 已经知道的",
      detail: "对它的 CLAUDE.md、AGENTS.md 和规则做第一天的整理",
    },
    agentsAnywhere: {
      title: "Agent 已经知道的",
      detail: "对它们在各处的文件做第一天的整理",
    },
    conventions: {
      title: "{space} 的约定",
      detail: "把它的约定复制过来作为提议",
    },
    nothing: "什么都不带",
  },
  conventionsLater: "从别的空间复制约定暂时还不能用。",
  foot: "一切都先作为提议，在你确认之前什么都不会保留。",
  cancel: "取消",
  create: "创建空间",
  created: "{space} 已就绪。连上一个 Agent 来填充它。",
};
