"use client";

import { useRouter } from "next/navigation";
import { X } from "lucide-react";

export default function PrivacyPage() {
  const router = useRouter();

  return (
    <main className="min-h-dvh bg-background">
      {/* Close — go back to landing page */}
      <button
        onClick={() => router.back()}
        className="fixed top-5 right-5 sm:top-8 sm:right-8 z-10 h-8 w-8 flex items-center justify-center rounded-full text-foreground/40 hover:text-foreground/60 hover:bg-foreground/5 transition-colors"
        aria-label="Close"
      >
        <X className="w-4 h-4" />
      </button>

      <article className="mx-auto max-w-2xl px-6 py-16 sm:py-24">
        {/* Header */}
        <div className="mb-12">
          <h1 className="text-[24px] font-bold text-foreground tracking-tight">
            Privacy Policy
          </h1>
          <p className="mt-2 text-[13px] text-foreground/35">
            Last updated: April 20, 2026
          </p>
        </div>

        {/* Content */}
        <div className="space-y-8 text-[15px] leading-[1.75] text-foreground/75">
          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              The short version
            </h2>
            <p>
              Your memories are yours. We store them so the Service works,
              process them with AI so they&rsquo;re searchable, and never sell
              them or use them to train anyone&rsquo;s models. Sub-processors
              are listed below. You can export or delete everything at any time.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Who we are
            </h2>
            <p>
              memax (&ldquo;memax&rdquo;, &ldquo;we&rdquo;, &ldquo;us&rdquo;) is
              operated by MemaxLabs, Inc. This policy explains what data we
              handle when you use memax.app, the memax CLI, the memax SDK, and
              our hosted MCP server.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              What we collect
            </h2>

            <p className="font-medium text-foreground mt-4 mb-1">
              Account information
            </p>
            <p>
              When you sign in with GitHub or Google, we receive your email,
              name, avatar URL, and the provider&rsquo;s stable user ID. If you
              link both providers, we keep both identities tied to one account.
              We do not receive your password.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              Your content
            </p>
            <p>
              Anything you push to memax: notes, transcripts, code snippets,
              files, and metadata you attach (titles, topics, source tags). This
              includes content captured by AI agents you connect (Claude Code,
              Codex, Cursor, ChatGPT, and similar) when you authorize them to
              write to memax on your behalf.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              File attachments
            </p>
            <p>
              Original files you attach to a memory (PDFs, images, documents)
              are uploaded directly from your browser to our object storage
              using short-lived signed URLs. We store the file, its filename,
              and content type.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              Hub and team data
            </p>
            <p>
              If you create or join a hub, we store the hub name, your role
              (owner, admin, member), and any invites you send. Memories you
              explicitly publish to a hub are visible to other hub members
              according to their role.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              API keys and agent grants
            </p>
            <p>
              When you generate an API key, we store a one-way hash of the key
              along with a short prefix for identification, the scopes you
              chose, and the time it was last used. We never store the raw key.
              When an AI agent connects via OAuth, we record the agent and the
              grant so you can revoke it later.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              Usage and operational logs
            </p>
            <p>
              We log API requests (including IP address and user agent) for
              reliability, debugging, abuse prevention, and rate limiting, and
              we log which memories were retrieved for a query so we can improve
              relevance. We do not record keystrokes, mouse movements, or
              browsing activity outside memax.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              Product analytics (optional)
            </p>
            <p>
              When enabled in our deployment, we use PostHog to record
              high-level events (page views, feature usage, errors) tied to your
              account ID. We do not send the contents of your memories, queries,
              or files to the analytics pipeline.
            </p>

            <p className="font-medium text-foreground mt-4 mb-1">
              Waitlist and pre-launch signups
            </p>
            <p>
              If you join the waitlist before getting an invite, we collect your
              email and the optional context you provide (use case, role, tools
              you use). We use this to prioritize invites and to send you status
              updates about your spot in line.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              How we use your data
            </h2>
            <ul className="space-y-1.5 list-none">
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To run the Service: store, index, search, and serve your
                  memories back to you and to agents you authorize.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To organize, summarize, and answer questions over your content
                  using the AI sub-processors listed below.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To meter usage against the limits of your plan and to bill you
                  (when paid plans are active).
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To send transactional email (sign-in confirmations, hub
                  invites, security notices, account changes). These are
                  required and cannot be opted out of while your account is
                  active.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To send occasional product updates and tips to the email on
                  your account. You can opt out at any time using the
                  unsubscribe link in any such email or from your account
                  settings.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To detect and prevent abuse, fraud, and security incidents.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  To comply with legal obligations and to enforce our Terms.
                </span>
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Sub-processors
            </h2>
            <p className="mb-3">
              We share data with the providers below only as needed to operate
              the Service. None of them are permitted to use your content to
              train their own models.
            </p>
            <ul className="space-y-2.5 list-none">
              <li>
                <strong className="text-foreground font-medium">
                  Anthropic
                </strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Claude models for classification, summarization, query
                rewriting, and answer synthesis. Receives the memory text or
                query needed for each operation. Anthropic does not train on API
                inputs or outputs.
              </li>
              <li>
                <strong className="text-foreground font-medium">
                  Voyage AI
                </strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Embeddings used to power semantic search. Receives memory text
                and queries. Voyage does not train on API inputs.
              </li>
              <li>
                <strong className="text-foreground font-medium">Resend</strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Transactional and product email delivery. Receives recipient
                email, subject, and message body.
              </li>
              <li>
                <strong className="text-foreground font-medium">
                  Cloudflare R2
                </strong>{" "}
                <span className="text-foreground/55">(global)</span> — Object
                storage for memory file attachments. Stores the files at rest.
              </li>
              <li>
                <strong className="text-foreground font-medium">Neon</strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Managed database hosting. Stores accounts, memories, and
                operational records.
              </li>
              <li>
                <strong className="text-foreground font-medium">Upstash</strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Managed cache for short-lived results, rate-limit counters, and
                usage metering.
              </li>
              <li>
                <strong className="text-foreground font-medium">Fly.io</strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Hosting for the API server and background worker.
              </li>
              <li>
                <strong className="text-foreground font-medium">Vercel</strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                Hosting for the memax.app web application and docs.memax.app
                developer hub.
              </li>
              <li>
                <strong className="text-foreground font-medium">PostHog</strong>{" "}
                <span className="text-foreground/55">
                  (United States, optional)
                </span>{" "}
                — Product analytics for high-level usage events. Disabled in
                deployments where the integration is not configured.
              </li>
              <li>
                <strong className="text-foreground font-medium">
                  GitHub, Google
                </strong>{" "}
                <span className="text-foreground/55">(United States)</span> —
                OAuth identity providers. We exchange tokens with them only when
                you sign in or link an account.
              </li>
            </ul>
            <p className="mt-3 text-foreground/55 text-[14px]">
              We&rsquo;ll update this list before adding a new sub-processor
              that handles personal data or memory content.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              How we don&rsquo;t use your data
            </h2>
            <ul className="space-y-1.5 list-none">
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>We don&rsquo;t sell your data.</span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  We don&rsquo;t use your content to train AI models &mdash;
                  ours or anyone else&rsquo;s.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>We don&rsquo;t show ads.</span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  We don&rsquo;t share your data with anyone outside the
                  sub-processors above, except when we&rsquo;re legally required
                  to (and we&rsquo;ll push back on overbroad requests).
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  We don&rsquo;t read your content for any purpose other than
                  running the Service. Engineers may access account data on a
                  need-to-know basis to debug a specific issue.
                </span>
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Access boundaries
            </h2>
            <p>
              Memories are private by default and invisible to other users
              unless you explicitly publish them to a hub. Within a hub, content
              is visible only to other hub members at the role you chose. We
              enforce account isolation at the data layer, not just in
              application code.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Security
            </h2>
            <p>
              All traffic is served over HTTPS. Data at rest is encrypted by our
              infrastructure providers. API keys are stored as one-way hashes,
              and sign-in sessions use short-lived access tokens.
            </p>
            <p className="mt-3">
              No system is perfectly secure. If you believe you&rsquo;ve found a
              vulnerability, email{" "}
              <a
                href="mailto:security@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                security@memaxlabs.com
              </a>{" "}
              and we&rsquo;ll respond within two business days.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Where your data lives
            </h2>
            <p>
              Our primary infrastructure is hosted in the United States. Some
              sub-processors (notably Cloudflare R2) operate globally and may
              cache or serve data from other regions. If you&rsquo;re in the
              EEA, the UK, or Switzerland, your data may be transferred to the
              United States; we rely on the providers&rsquo; standard
              contractual clauses for those transfers.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              How long we keep things
            </h2>
            <ul className="space-y-1.5 list-none">
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Memories, files, and account data: until you delete them or
                  close your account.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Deleted agent configs are retained in a tombstoned state for
                  30 days so you can restore them, then permanently removed.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Operational logs and short-lived caches: kept only as long as
                  needed to operate the Service.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Closed accounts: content is removed from active systems within
                  30 days of confirmation. Backups roll off on their normal
                  cycle (up to 30 additional days).
                </span>
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Your rights
            </h2>
            <ul className="space-y-1.5 list-none">
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    Access:
                  </strong>{" "}
                  View any memory through the app, CLI, or API.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    Export:
                  </strong>{" "}
                  Download your memories as plain text or markdown via the CLI
                  or API. There is no proprietary lock-in.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    Correct:
                  </strong>{" "}
                  Edit any memory or your profile from the app.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    Delete:
                  </strong>{" "}
                  Remove individual memories at any time, or request full
                  account deletion (see Contact below).
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    Object &amp; restrict:
                  </strong>{" "}
                  If you&rsquo;re in the EEA, the UK, Switzerland, California,
                  or another jurisdiction with applicable privacy law, you can
                  ask us to restrict or stop processing your data, withdraw
                  consent, or lodge a complaint with your local data protection
                  authority.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  <strong className="text-foreground font-medium">
                    No sale, no &ldquo;sharing&rdquo;:
                  </strong>{" "}
                  We do not sell personal information and do not share it for
                  cross-context behavioral advertising under the CCPA/CPRA.
                </span>
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Cookies and local storage
            </h2>
            <p>
              We don&rsquo;t set tracking cookies. The web app uses your
              browser&rsquo;s localStorage to keep your sign-in token, the
              currently active hub, and your interface preferences (locale,
              recent navigation). When PostHog is enabled, it stores its own
              anonymous distinct ID in localStorage. Sign-out clears the memax
              keys.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Children
            </h2>
            <p>
              memax is not directed to children under 13 (or under 16 in the
              EEA), and we do not knowingly collect data from them. If you
              believe a child has created an account, email us and we&rsquo;ll
              delete it.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Changes
            </h2>
            <p>
              We may update this policy. If we make a change that materially
              reduces your rights or expands how we use your data, we&rsquo;ll
              tell you in the app or by email before it takes effect. The
              &ldquo;last updated&rdquo; date above always reflects the current
              version.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              Contact
            </h2>
            <p>
              Privacy questions, data export requests, or account deletion
              requests:{" "}
              <a
                href="mailto:privacy@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                privacy@memaxlabs.com
              </a>
              . General questions:{" "}
              <a
                href="mailto:team@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                team@memaxlabs.com
              </a>
              . We aim to respond within five business days and to complete
              verified deletion requests within 30 days.
            </p>
          </section>
        </div>
      </article>
    </main>
  );
}
