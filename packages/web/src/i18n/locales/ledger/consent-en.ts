// OAuthConsent (plan 25 §5.15, epic 2.3): a person lets an outside agent
// connect to one space. The board's copy; {client} is the name the client
// gives itself, shortened where a line can't wrap.
export const ledgerConsentEn = {
  pageTitle: "Connect an agent",
  title: "{client} wants to connect to Memax",
  signedInAs: "Signed in as {name}",
  notYou: "Not you?",
  servedFrom: "from {host}",
  servedFromLabel: "Memax read this agent's details from {host}",
  which: "Which space",
  meta: {
    project: "Project",
    projectMeta: "Project · {memories}",
    team: "Team",
    teamMeta: "Team · {people}",
    personal: "Just you",
    memories: "{n} memories",
    memoriesOne: "1 memory",
    people: "{n} people",
    peopleOne: "1 person",
    onV1: "{meta} · still on V1",
  },
  compiles: "compiles {file}",
  disabled: "Your role here can't use what {client} asked for.",
  will: "{client} will be able to",
  wont: "It won't be able to",
  can: {
    read_brief: "Read the Brief and kept memories",
    read_memories: "Read the memories in it",
    propose: "Propose memories, which wait for you",
    keep: "Keep memories without asking you",
    add: "Add memories, which are kept as written",
    gate: "Ask you to decide at a fork",
    forget: "Forget memories",
    other_spaces: "See your other spaces",
  },
  cannot: {
    read_brief: "Read the Brief",
    read_memories: "Read the memories in it",
    propose: "Propose memories",
    keep: "Keep anything without you",
    add: "Add memories",
    gate: "Ask you to decide at a fork",
    forget: "Forget anything",
    other_spaces: "See your other spaces",
  },
  cancel: "Cancel",
  allow: "Allow {client}",
  footnote: "You can allow Write, or disconnect {client}, any time in Agents.",
  footnoteRead:
    "You can let {client} propose or write, or disconnect it, any time in Agents.",
  footnoteReadPropose:
    "You can let {client} propose, or disconnect it, any time in Agents.",
  footnoteDisconnect: "You can disconnect {client} any time in Agents.",
  errors: {
    space:
      "That space can't be connected from this account. Choose one of the spaces here.",
    failed: "That didn't go through. Try again.",
  },
  loading: "Loading the request",
  missing: {
    title: "This link has no request in it.",
    lede: "Start connecting again from your agent. It opens this page with its request.",
  },
  expired: {
    title: "This request expired.",
    lede: "Requests last 10 minutes. Start connecting again from your agent.",
  },
  gone: {
    title: "This request has ended.",
    lede: "It was answered already, someone else opened it, or this is an old link. If your agent is still waiting, start connecting again from it.",
  },
  handed: {
    title: "Back to {client}.",
    lede: "Your browser handed the answer to {client}. You can close this tab.",
  },
  refused: {
    title: "This request can't be answered from here.",
    lede: "Sign in to the Memax web app as yourself, then start connecting again from your agent.",
  },
  failed: {
    title: "This request didn't load.",
    lede: "Check your connection and try again. Requests last 10 minutes.",
    retry: "Try again",
  },
  noSpaces: {
    title: "You don't have a space yet.",
    lede: "Set one up, then connect {client} again from your agent.",
    setup: "Set up Memax",
  },
  fallbackClient: "An agent",
} as const;
