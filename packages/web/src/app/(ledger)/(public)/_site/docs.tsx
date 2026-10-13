"use client";

import Link from "next/link";
import { useId, useMemo, useState } from "react";
import { Button, Field, Icon, Logo, formatNodes } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import type { Translations } from "@/i18n/locales/en";
import { cliCommand } from "@/lib/v2/cli";
import { MCP_URL } from "@/lib/urls";
import { docsPage, LINKS } from "./links";
import styles from "./docs.module.css";

type TreeKey = keyof Translations["ledger"]["public"]["docs"]["tree"];

/**
 * The tree's pages: this quickstart, then the docs site's pages
 * (docs.memax.app) by their paths there. public-docs.test.ts checks each
 * path is a page of packages/docs-site.
 */
export const DOCS_TREE: {
  group: TreeKey;
  pages: { key: TreeKey; path: string | null }[];
}[] = [
  {
    group: "start",
    pages: [
      { key: "quickstart", path: null },
      { key: "install", path: "/quickstart/installation" },
      { key: "personal", path: "/concepts/personal-layer" },
    ],
  },
  {
    group: "agents",
    pages: [
      { key: "claudeCode", path: "/integrations/claude-code" },
      { key: "plugin", path: "/integrations/claude-code-plugin" },
      { key: "codex", path: "/integrations/codex" },
      { key: "cursor", path: "/integrations/cursor" },
      { key: "cloud", path: "/integrations/cloud-agents" },
      { key: "otherAgents", path: "/integrations/other-agents" },
    ],
  },
  {
    group: "concepts",
    pages: [
      { key: "memories", path: "/concepts/memories" },
      { key: "dream", path: "/concepts/dreams" },
      { key: "security", path: "/concepts/security" },
      { key: "exportFormat", path: "/concepts/export" },
    ],
  },
  {
    group: "reference",
    pages: [
      { key: "cli", path: "/cli" },
      { key: "init", path: "/cli/init" },
      { key: "compile", path: "/cli/compile" },
      { key: "connect", path: "/cli/connect" },
      { key: "mcp", path: "/cli/mcp" },
    ],
  },
];

/**
 * The MCP tools, in the order the quickstart lists them. Every tool of
 * the catalogue (packages/server/internal/handler/mcp_tools.json), which
 * public-docs.test.ts holds this list to.
 */
export const DOCS_TOOLS = [
  "memax_recall",
  "memax_search",
  "memax_get",
  "memax_list",
  "memax_push",
  "memax_capture",
  "memax_request_decision",
  "memax_forget",
  "memax_hubs",
  "memax_hub_members",
  "memax_topics",
] as const;

const SECTIONS = ["init", "settle", "compile", "cloud", "tools"] as const;

/**
 * Docs · quickstart (Docs.png): the four steps from the files your agents
 * keep to a compiled Brief, and the MCP tools, beside the docs site's
 * tree. The steps say what the CLI does today: init settles in the
 * terminal (there is no `memax review`), pull-request delivery isn't
 * built, and the remote MCP server is this build's API.
 */
export function DocsScreen() {
  const { t } = useLocale();
  const d = t.ledger.public.docs;
  const [query, setQuery] = useState("");
  const statusId = useId();
  return (
    <div className={styles.page}>
      <a className="mx-skip" href="#content">
        {t.ledger.public.site.skip}
      </a>
      <header className={styles.header}>
        <Link
          href={LINKS.landing}
          className={styles.home}
          aria-label={t.ledger.public.site.home}
        >
          <Logo variant="lockup" size={18} decorative />
        </Link>
        <span className={styles.section}>{d.header.label}</span>
        <Field
          type="search"
          icon="search"
          placeholder={d.header.search}
          aria-label={d.header.search}
          aria-controls="docs-tree"
          aria-describedby={query.trim() ? statusId : undefined}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className={styles.search}
        />
        <Button
          variant="primary"
          size="sm"
          href={LINKS.signIn}
          className={styles.open}
        >
          {d.header.open}
        </Button>
      </header>
      <div className={styles.body}>
        <DocsTree query={query} statusId={statusId} />
        <main id="content" tabIndex={-1} className={styles.article}>
          <p className={styles.eyebrow}>{d.eyebrow}</p>
          <h1 className={styles.title}>{d.title}</h1>
          <p className={styles.lede}>{d.lede}</p>

          <h2 id="init">{d.init.title}</h2>
          <p>{d.init.body}</p>
          <Command text={cliCommand("init")} />

          <h2 id="settle">{d.settle.title}</h2>
          <p>{d.settle.body}</p>

          <h2 id="compile">{d.compile.title}</h2>
          <p>{d.compile.body}</p>
          <Command text="memax compile" />

          <h2 id="cloud">{d.cloud.title}</h2>
          <p>{d.cloud.body}</p>
          <Command text={MCP_URL} prompt={false} />
          <p>{d.cloud.start}</p>
          <Command text="memax connect --cloud copilot" />

          <h2 id="tools">{d.tools.title}</h2>
          <div className={`mx-panel ${styles.tools}`} role="table">
            <div className={styles.toolHead} role="row">
              <span role="columnheader">{d.tools.tool}</span>
              <span role="columnheader">{d.tools.does}</span>
            </div>
            {DOCS_TOOLS.map((tool) => (
              <div key={tool} className={styles.tool} role="row">
                <span role="rowheader">
                  <code className="mx-code">{tool}</code>
                </span>
                <span role="cell">{d.tools[tool]}</span>
              </div>
            ))}
          </div>
          <div className={styles.note}>
            <Icon name="shield" size={16} />
            <span>{d.note}</span>
          </div>
          <div className={styles.next}>
            <a href={docsPage("/cli/init")}>
              <span className="mx-meta">{d.next}</span>
              <span className={styles.nextTitle}>{d.tree.init}</span>
            </a>
          </div>
        </main>
        <nav aria-labelledby="toc-title" className={styles.toc}>
          <p id="toc-title" className="mx-section-label">
            {d.toc.label}
          </p>
          <ul>
            {SECTIONS.map((id) => (
              <li key={id}>
                <a href={`#${id}`}>{d.toc[id]}</a>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </div>
  );
}

/** A command on night, as the quickstart draws it. */
function Command({ text, prompt = true }: { text: string; prompt?: boolean }) {
  return (
    <pre className={styles.cmd}>
      {prompt ? (
        <span className={styles.prompt} aria-hidden="true">
          ›{" "}
        </span>
      ) : null}
      <code>{text}</code>
    </pre>
  );
}

/**
 * The docs' pages, filtered by "Search the docs" in the header: a title
 * search over this tree, said in a polite status, with the full docs
 * site when nothing here matches.
 */
function DocsTree({ query, statusId }: { query: string; statusId: string }) {
  const { t, locale } = useLocale();
  const d = t.ledger.public.docs;
  const q = query.trim().toLocaleLowerCase(locale);
  const groups = useMemo(
    () =>
      DOCS_TREE.map(({ group, pages }) => ({
        group,
        pages: pages.filter(
          ({ key }) => !q || d.tree[key].toLocaleLowerCase(locale).includes(q),
        ),
      })).filter(({ pages }) => pages.length > 0),
    [d.tree, locale, q],
  );
  const matches = groups.reduce((n, g) => n + g.pages.length, 0);
  const site = new URL(LINKS.fullDocs).host;
  return (
    <nav id="docs-tree" aria-label={d.tree.label} className={styles.tree}>
      <p id={statusId} className={styles.status} role="status">
        {!q
          ? ""
          : matches === 0
            ? formatNodes(d.header.noMatch, {
                site: <a href={LINKS.fullDocs}>{site}</a>,
              })
            : matches === 1
              ? d.header.matchesOne
              : interpolate(d.header.matches, { n: matches })}
      </p>
      {groups.map(({ group, pages }) => (
        <div key={group} className={styles.group}>
          <p className={styles.groupLabel}>{d.tree[group]}</p>
          <ul>
            {pages.map(({ key, path }) => (
              <li key={key}>
                {path === null ? (
                  <a href="#content" aria-current="page">
                    {d.tree[key]}
                  </a>
                ) : (
                  <a href={docsPage(path)}>{d.tree[key]}</a>
                )}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}
