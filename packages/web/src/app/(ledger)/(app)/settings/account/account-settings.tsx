"use client";

import { useEffect, useRef } from "react";
import { useSearchParams } from "next/navigation";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { linkErrorText } from "@/lib/v2/account-copy";
import { useAccount } from "../../_lib/account";
import { PlaceSkeleton } from "../../_components/skeleton";
import { useToast } from "../../_components/toasts";
import { ThemeControl } from "../../../_components/theme-control";
import { ForgetAccountPanel } from "./forget-account";
import { ProfilePanel } from "./profile-panel";
import { SessionsPanel } from "./sessions-panel";
import { SignInPanel } from "./sign-in-panel";

/**
 * Settings › Account (Account.png, plan §5.15, epic 2.6): Profile (the
 * name receipts show, the email sign-in codes go to, Dream's time zone,
 * this device's language), Sign in with (GitHub, Google, passkeys),
 * Signed in on (every session, with sign-out), and the danger zone,
 * forgetting the account. The theme is this device's, beside the title.
 *
 * Coming back from a provider's page, `?account_linked=` or
 * `?account_link_error=` says how linking went.
 */
export function AccountSettings() {
  const { t } = useLocale();
  const copy = t.ledger.account;
  const account = useAccount();
  const params = useSearchParams();
  const toast = useToast();
  const told = useRef(false);

  const linked = params?.get("account_linked");
  const linkError = params?.get("account_link_error");
  useEffect(() => {
    if (told.current || (!linked && !linkError)) return;
    told.current = true;
    const provider = (p: string | null | undefined) =>
      p === "google" ? copy.signIn.google : copy.signIn.github;
    toast(
      linked
        ? {
            state: "kept",
            text: interpolate(copy.signIn.linked, {
              provider: provider(linked),
            }),
          }
        : {
            text: interpolate(copy.signIn.linkFailed, {
              provider: provider(params?.get("provider")),
              reason: linkErrorText(copy, linkError ?? "other"),
            }),
          },
    );
    // Drop the parameter so a reload doesn't say it again.
    const url = new URL(window.location.href);
    url.searchParams.delete("account_linked");
    url.searchParams.delete("account_link_error");
    window.history.replaceState(null, "", url.pathname + url.search + url.hash);
  }, [linked, linkError, params, copy, toast]);

  const header = (
    <PageHeader
      title={copy.title}
      lede={copy.lede}
      actions={<ThemeControl size="sm" />}
    />
  );

  if (!account.data) {
    return (
      <>
        {header}
        {account.isError ? (
          <section className="mx-panel" role="alert">
            <header className="mx-panel-head">
              <h2 className="mx-panel-title">{copy.loadFailed}</h2>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void account.refetch()}
              >
                {copy.retry}
              </Button>
            </header>
          </section>
        ) : (
          <PlaceSkeleton
            title={copy.title}
            status={copy.loading}
            label={copy.loading}
          />
        )}
      </>
    );
  }

  return (
    <>
      {header}
      <ProfilePanel account={account.data} />
      <SignInPanel
        account={account.data}
        addRequested={params?.get("add") === "passkey"}
      />
      <SessionsPanel />
      <ForgetAccountPanel account={account.data} />
    </>
  );
}
