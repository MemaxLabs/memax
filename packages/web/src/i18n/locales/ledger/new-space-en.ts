// New space (NewSpace.png), mounted as `t.ledger.newSpace`: the board's
// copy, and what it says where the API only makes project spaces. Why a
// space wasn't made is worded in records-copy.ts (`spaceFailureText`).
// Placeholders are {name}.
export const ledgerNewSpaceEn = {
  title: "New space",
  close: "Close",
  kind: "Kind",
  kindLabel: "Kind of space",
  kinds: {
    personal: {
      title: "Personal",
      detail: "Just you: how you like to work, across every tool.",
    },
    project: {
      title: "Project",
      detail: "One codebase. Compiles into its repository.",
    },
    team: {
      title: "Team",
      detail: "People and their agents, with roles and decisions.",
    },
  },
  // Each person has one personal space, made with their account.
  personalExists: "You have one already: {space}. Each person has one.",
  openPersonal: "Open {space}",
  teamLater: "Team spaces come with Team. Make a project space for now.",
  name: "Name",
  nameRequired: "Name the space.",
  nameTooLong: "A name is at most 80 characters.",
  repository: "Repository",
  repositoryPlaceholder: "owner/name",
  repositoryInvalid: "Write it as owner/name, such as MemaxLabs/memax-web.",
  belongsTo: "Belongs to",
  justMe: "Just me",
  // A team owning a project space comes with Team too.
  teamOwnerLater: "Spaces that belong to a team come with Team.",
  startFrom: "Start from",
  start: {
    agents: {
      title: "What agents already know in this repository",
      detail: "Runs the day-one cleanup on its CLAUDE.md, AGENTS.md and rules",
    },
    agentsAnywhere: {
      title: "What agents already know",
      detail: "Runs the day-one cleanup on their files, wherever they work",
    },
    conventions: {
      title: "Conventions from {space}",
      detail: "Copies its conventions as proposals",
    },
    nothing: "Nothing",
  },
  conventionsLater: "Copying another space's conventions isn't here yet.",
  foot: "Everything starts as a proposal. Nothing is kept until you say so.",
  cancel: "Cancel",
  create: "Create space",
  // The toast once it's made.
  created: "{space} is ready. Connect an agent to fill it.",
};
