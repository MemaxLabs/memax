// The secret scan memax init runs before anything leaves the machine
// (plan 25 §7.3 step 4, §5.16, HANDOFF rule 8). Two rule sets, both local:
//
//   - secretlint's recommended preset (MIT; about 30 vendors' token
//     formats, maintained upstream), over each file's whole text;
//   - the server's own refusal patterns (packages/server/internal/secrets,
//     ported line for line), over each statement, so nothing the server
//     would refuse is ever sent to it. The shared corpus in
//     test/init/secrets-corpus.json holds both sides to the same cases.
//
// A statement that holds a secret never leaves the machine; the import
// says where it was and which rule matched, never the secret.

/** One secret found: where, and which rule (a name, never the value). */
export interface SecretHit {
  line: number;
  rule: string;
}

/** The server's push-refusal patterns (internal/secrets.credentialPatterns). */
export const SERVER_PATTERNS: Array<{ name: string; rx: RegExp }> = [
  {
    name: "vendor API key",
    rx: /\b(?:sk|pk|xoxb|xoxa|xoxp|xoxs|xapp|ghp|gho|ghu|ghs|ghr|github_pat)[_-][A-Za-z0-9_-]{16,}\b/i,
  },
  { name: "Stripe live key", rx: /\b(?:sk|pk|rk)_live_[A-Za-z0-9]{16,}\b/i },
  { name: "AWS access key", rx: /\bAKIA[0-9A-Z]{16}\b/ },
  { name: "Google API key", rx: /\bAIza[0-9A-Za-z_-]{35}\b/ },
  { name: "private key block", rx: /-----BEGIN[^-]*?PRIVATE KEY-----/s },
  {
    name: "JWT",
    rx: /\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/,
  },
  {
    name: "credential assignment",
    rx: /\b(?:client_secret|api[_-]?key|secret|token|password|passwd|authorization)\s*[:=]\s*["']?[A-Za-z0-9._\-+/]{16,}["']?/i,
  },
  { name: "bearer token", rx: /\bBearer\s+[A-Za-z0-9._-]{20,}\b/i },
];

/** The names of the server's patterns a text matches, in order. */
export function serverRefusals(text: string): string[] {
  return SERVER_PATTERNS.filter((p) => p.rx.test(text)).map((p) => p.name);
}

const VENDOR: Record<string, string> = {
  "1password": "1Password token",
  anthropic: "Anthropic API key",
  aws: "AWS credential",
  basicauth: "password in a URL",
  cloudflare: "Cloudflare token",
  "database-connection-string": "database password",
  databricks: "Databricks token",
  docker: "Docker token",
  figma: "Figma token",
  gcp: "Google Cloud key",
  github: "GitHub token",
  gitlab: "GitLab token",
  grafana: "Grafana token",
  groq: "Groq API key",
  "hashicorp-vault": "Vault token",
  huggingface: "Hugging Face token",
  linear: "Linear API key",
  notion: "Notion token",
  npm: "npm token",
  openai: "OpenAI API key",
  privatekey: "private key",
  sendgrid: "SendGrid key",
  shopify: "Shopify key",
  slack: "Slack token",
  stripe: "Stripe key",
  tailscale: "Tailscale key",
  vercel: "Vercel token",
};

/** A rule's name for people, from secretlint's rule id. */
export function ruleName(ruleId: string): string {
  const vendor = ruleId.replace(/^@secretlint\/secretlint-rule-/, "");
  return VENDOR[vendor] ?? `${vendor.replace(/-/g, " ")} credential`;
}

type LintSource = (o: {
  source: { filePath: string; content: string; contentType: "text" };
  options: {
    config: { rules: Array<{ id: string; rule: unknown }> };
    maskSecrets?: boolean;
    noPhysicFilePath?: boolean;
  };
}) => Promise<{
  messages: Array<{
    ruleId: string;
    loc: { start: { line: number }; end: { line: number } };
  }>;
}>;

let loaded: Promise<{ lint: LintSource; preset: unknown }> | null = null;

/** secretlint, loaded on first use (it isn't on any other command's path). */
function secretlint(): Promise<{ lint: LintSource; preset: unknown }> {
  loaded ??= Promise.all([
    import("@secretlint/core"),
    import("@secretlint/secretlint-rule-preset-recommend"),
  ]).then(([core, preset]) => ({
    lint: core.lintSource as unknown as LintSource,
    preset: preset.creator,
  }));
  return loaded;
}

/** Every line of a file that holds a secret, by either rule set. */
export async function scanSecrets(
  content: string,
  label: string,
): Promise<SecretHit[]> {
  const hits: SecretHit[] = [];
  const { lint, preset } = await secretlint();
  const res = await lint({
    source: { filePath: label, content, contentType: "text" },
    options: {
      config: {
        rules: [
          { id: "@secretlint/secretlint-rule-preset-recommend", rule: preset },
        ],
      },
      maskSecrets: true,
      noPhysicFilePath: true,
    },
  });
  for (const m of res.messages) {
    for (let l = m.loc.start.line; l <= m.loc.end.line; l++)
      hits.push({ line: l, rule: ruleName(m.ruleId) });
  }
  const lines = content.replace(/^\u{FEFF}/u, "").split(/\r\n|\r|\n/);
  let inKey = false;
  lines.forEach((line, i) => {
    // A private key's whole block, not just its BEGIN line.
    if (/-----BEGIN[^-]*?PRIVATE KEY-----/.test(line)) inKey = true;
    const names = inKey ? ["private key block"] : serverRefusals(line);
    for (const name of names) hits.push({ line: i + 1, rule: name });
    if (/-----END[^-]*?PRIVATE KEY-----/.test(line)) inKey = false;
  });
  return hits;
}

/** The secrets a statement holds: its lines' hits, and the server's patterns over its joined words. */
export function secretsIn(
  hits: SecretHit[],
  start: number,
  end: number,
  text: string,
): string[] {
  const names = new Set<string>();
  for (const h of hits) if (h.line >= start && h.line <= end) names.add(h.rule);
  for (const n of serverRefusals(text)) names.add(n);
  return [...names];
}
