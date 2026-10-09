"use client";

import { useRouter } from "next/navigation";
import { X } from "lucide-react";

export default function TermsPage() {
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
            Terms of Service
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
              You own everything you put in memax. We give you a license-back to
              operate the Service. Don&rsquo;t use memax for anything illegal or
              to break other people&rsquo;s things. We don&rsquo;t promise the
              Service will never break, but we&rsquo;ll work to keep it running.
              The longer version is below.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              1. The Service
            </h2>
            <p>
              memax (&ldquo;Service&rdquo;) is a personal and team memory layer
              for AI agents, operated by MemaxLabs, Inc.
              (&ldquo;MemaxLabs&rdquo;, &ldquo;we&rdquo;, &ldquo;us&rdquo;). The
              Service includes the memax.app web application, the memax CLI
              (published as{" "}
              <code className="text-[13px] font-mono">memax-cli</code> on npm),
              the memax SDK, our hosted MCP server, and the developer
              documentation at docs.memax.app. By creating an account or using
              the Service, you agree to these Terms and to our{" "}
              <a
                href="/privacy"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                Privacy Policy
              </a>
              .
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              2. Eligibility
            </h2>
            <p>
              You must be at least 13 years old (16 in the EEA) to use memax. If
              you use the Service on behalf of an organization, you confirm you
              have authority to bind that organization to these Terms. memax is
              not a HIPAA Business Associate and is not designed for regulated
              health, payment-card, or government-classified data. Don&rsquo;t
              put that kind of data in memax.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              3. Your account
            </h2>
            <p>
              You sign in via GitHub or Google OAuth. You&rsquo;re responsible
              for keeping that identity provider account secure and for any
              activity under your memax account. Notify us at{" "}
              <a
                href="mailto:security@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                security@memaxlabs.com
              </a>{" "}
              if you suspect unauthorized access. One person, one account
              &mdash; don&rsquo;t share login credentials.
            </p>
            <p className="mt-3">
              You can generate API keys to let scripts and AI agents act on your
              behalf. API keys carry the scopes and hub access you choose.
              You&rsquo;re responsible for what those keys do; revoke them from
              Settings if they leak.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              4. Your content
            </h2>
            <p>
              You retain full ownership of all content you store in memax
              (&ldquo;Your Content&rdquo;). We claim no intellectual property
              rights over Your Content.
            </p>
            <p className="mt-3">
              You grant MemaxLabs a worldwide, non-exclusive, royalty-free
              license to host, copy, transmit, index, embed, summarize, and
              display Your Content solely as needed to operate the Service for
              you and the people you share it with. This license ends when you
              delete the content or close your account, except for residual
              copies in routine backups (which roll off on their normal cycle).
            </p>
            <p className="mt-3">
              You represent that you have the right to store any content you
              push to memax, and that doing so does not violate any third-party
              rights or applicable law.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              5. AI processing
            </h2>
            <p>
              memax sends Your Content to the third-party AI providers listed in
              our Privacy Policy only as needed to operate features you use
              &mdash; classification, summarization, semantic search, and answer
              synthesis. Those providers are contractually bound not to use Your
              Content to train their models, and we don&rsquo;t use it to train
              models either.
            </p>
            <p className="mt-3">
              AI output is generated automatically and may be wrong, incomplete,
              or misleading. You&rsquo;re responsible for reviewing AI-generated
              answers before relying on them. memax is not a source of
              professional advice.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              6. Hubs and shared content
            </h2>
            <p>
              You can create or join hubs to share memories with other people.
              When you publish a memory to a hub, every member of that hub at
              the relevant role can read, search, and export it. Hub owners and
              admins can invite, remove, and change the role of other members,
              and can remove memories from the hub.
            </p>
            <p className="mt-3">
              If you leave a hub or are removed, content you published to that
              hub remains accessible to remaining members unless you delete it
              first or the owner removes it. Memories you keep private to your
              personal scope are unaffected.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              7. Acceptable use
            </h2>
            <p className="mb-2">You won&rsquo;t use memax to:</p>
            <ul className="space-y-1.5 list-none">
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Store or distribute content that is illegal, infringing,
                  defamatory, or that violates someone else&rsquo;s rights.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Store malware, phishing payloads, or material that exploits or
                  endangers minors.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Try to access another user&rsquo;s account or memories, probe
                  the system for vulnerabilities outside a coordinated security
                  disclosure, or circumvent rate limits and quota.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Reverse-engineer or scrape the Service to build a competing
                  product, or to train a foundation model.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Use the Service to send unsolicited messages, spam other
                  users, or abuse our email sending infrastructure.
                </span>
              </li>
              <li className="flex gap-2">
                <span className="text-foreground/20 shrink-0">&bull;</span>
                <span>
                  Resell access to the Service without a written agreement with
                  us.
                </span>
              </li>
            </ul>
            <p className="mt-3">
              We may suspend or terminate accounts that violate these rules. For
              clear-cut abuse (e.g., CSAM, active attacks) we may act without
              notice.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              8. Plans, billing, and trials
            </h2>
            <p>
              memax offers a free tier with usage limits, paid personal plans,
              and team plans. Current pricing and limits are shown on memax.app
              and may change with notice.
            </p>
            <p className="mt-3">
              While the Service is in pre-launch, paid plans may be administered
              manually rather than through automated billing. When automated
              billing is enabled, fees are charged in advance for the billing
              period you choose, are non-refundable except where required by
              law, and renew automatically until you cancel. You can cancel at
              any time from Settings; cancellation stops future renewals and
              your account drops to the free tier at the end of the paid period.
            </p>
            <p className="mt-3">
              If you exceed the limits of your plan, we may rate-limit or queue
              further requests, prompt you to upgrade, or pause non-essential
              processing (such as nightly memory consolidation) until the next
              billing period.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              9. Beta features
            </h2>
            <p>
              Some features are labeled beta, preview, or experimental. They may
              change, break, or be removed without notice, and they don&rsquo;t
              carry the same availability expectations as the rest of the
              Service.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              10. Third-party integrations
            </h2>
            <p>
              memax connects to AI agents, code editors, and other tools at your
              direction. Those tools are operated by their own providers under
              their own terms; we&rsquo;re not responsible for what they do with
              data you authorize them to read or write. You can revoke any
              agent&rsquo;s access from Settings at any time.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              11. Service availability
            </h2>
            <p>
              We aim for high availability and design for graceful degradation,
              but we don&rsquo;t guarantee uninterrupted or error-free
              operation. We&rsquo;ll do reasonable maintenance and may schedule
              brief downtime; we&rsquo;ll try to give notice for anything more
              than minutes. The Service is provided &ldquo;as is&rdquo; and
              &ldquo;as available.&rdquo;
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              12. Deleting your data and closing your account
            </h2>
            <p>
              You can delete individual memories at any time through the app,
              CLI, or API. To close your account and delete all associated data,
              email{" "}
              <a
                href="mailto:privacy@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                privacy@memaxlabs.com
              </a>
              . We process verified deletion requests within 30 days. Backups
              roll off on their normal cycle (up to 30 additional days). We may
              retain a minimal record of the deletion itself (e.g., that an
              account with a given email was closed) for fraud prevention and
              legal compliance.
            </p>
            <p className="mt-3">
              We may suspend or terminate your account if you materially breach
              these Terms, if required by law, or if your account is dormant for
              an extended period after notice.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              13. Copyright and IP claims
            </h2>
            <p>
              If you believe content stored in memax infringes your copyright or
              other IP rights, send a notice to{" "}
              <a
                href="mailto:legal@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                legal@memaxlabs.com
              </a>{" "}
              including the work at issue, where it appears in memax, your
              contact information, and a good-faith statement of ownership.
              We&rsquo;ll review and respond consistent with the DMCA and
              applicable law.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              14. Disclaimers
            </h2>
            <p>
              Except where prohibited by law, the Service is provided without
              warranties of any kind, whether express or implied, including
              merchantability, fitness for a particular purpose, and
              non-infringement. We don&rsquo;t warrant that the Service will
              meet your requirements, that AI outputs will be accurate, or that
              data will never be lost. Back up anything you can&rsquo;t afford
              to lose.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              15. Limitation of liability
            </h2>
            <p>
              To the maximum extent permitted by law, MemaxLabs and its
              officers, employees, and contractors will not be liable for any
              indirect, incidental, special, consequential, or punitive damages,
              or for lost profits, lost data, or loss of goodwill, arising out
              of or related to the Service. Our aggregate liability for any
              claim arising out of or related to the Service is limited to the
              greater of (a) US$100 or (b) the amount you paid us for the
              Service in the twelve months preceding the event giving rise to
              the claim.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              16. Indemnification
            </h2>
            <p>
              You&rsquo;ll defend and indemnify MemaxLabs against third-party
              claims arising out of (a) Your Content, (b) your use of the
              Service in violation of these Terms or applicable law, or (c) your
              infringement of another party&rsquo;s rights. We&rsquo;ll tell you
              about any such claim and cooperate reasonably in the defense.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              17. Governing law and disputes
            </h2>
            <p>
              These Terms are governed by the laws of the State of Delaware,
              without regard to its conflict-of-laws rules. The exclusive venue
              for any dispute that can&rsquo;t be resolved informally is the
              state and federal courts located in Delaware, and you and
              MemaxLabs consent to personal jurisdiction there. Nothing in this
              section prevents either party from seeking injunctive relief in
              any court of competent jurisdiction. If you&rsquo;re a consumer in
              a jurisdiction whose law gives you the non-waivable right to bring
              claims locally, this section doesn&rsquo;t override that right.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              18. Changes to these terms
            </h2>
            <p>
              We may update these Terms. If a change is material, we&rsquo;ll
              notify you in the app or by email at least 14 days before it takes
              effect. Continued use after the effective date means you accept
              the change. If you don&rsquo;t accept, stop using the Service and
              (if you want) close your account.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              19. Miscellaneous
            </h2>
            <p>
              These Terms, together with the Privacy Policy and any plan or
              order documents, are the entire agreement between you and
              MemaxLabs about the Service. If a court finds any provision
              unenforceable, the rest stays in effect. Our failure to enforce a
              right isn&rsquo;t a waiver of it. You can&rsquo;t assign these
              Terms without our written consent; we may assign them in
              connection with a merger, acquisition, or sale of assets.
            </p>
          </section>

          <section>
            <h2 className="text-[16px] font-semibold text-foreground mb-3">
              20. Contact
            </h2>
            <p>
              Questions about these Terms:{" "}
              <a
                href="mailto:legal@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                legal@memaxlabs.com
              </a>
              . For everything else:{" "}
              <a
                href="mailto:team@memaxlabs.com"
                className="text-foreground underline underline-offset-2 hover:text-foreground/60 transition-colors"
              >
                team@memaxlabs.com
              </a>
              .
            </p>
          </section>
        </div>
      </article>
    </main>
  );
}
