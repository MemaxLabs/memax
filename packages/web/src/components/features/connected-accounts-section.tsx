"use client";

import type { AuthIdentity, AuthProviderName } from "memax-sdk";
import type { ReactNode } from "react";
import { ContentError } from "@memaxlabs/ui";
import {
  useAuthIdentities,
  useStartProviderLink,
  useUnlinkProvider,
} from "@/hooks/use-auth-identities";
import { useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";

type ProviderMeta = {
  provider: AuthProviderName;
  label: string;
  Icon: (props: { className?: string }) => ReactNode;
};

export function ConnectedAccountsSection() {
  const { t } = useLocale();
  const identities = useAuthIdentities();
  const startLink = useStartProviderLink();
  const unlink = useUnlinkProvider();
  const { refreshProfile } = useAuth();
  const providers: ProviderMeta[] = [
    {
      provider: "github",
      label: t.userSettings.providerGithub,
      Icon: GitHubIcon,
    },
    {
      provider: "google",
      label: t.userSettings.providerGoogle,
      Icon: GoogleIcon,
    },
  ];

  if (identities.isError) {
    return (
      <ContentError
        message={t.states.error.network}
        onRetry={() => identities.refetch()}
      />
    );
  }

  const rows = identities.data ?? [];
  const loading = identities.isLoading;

  return (
    <div>
      <div className="px-1 pb-3">
        <p className="text-[13px] text-fg-3">
          {t.userSettings.connectedAccountsDesc}
        </p>
      </div>

      <div className="space-y-1">
        {providers.map((provider) => {
          const identity = rows.find(
            (row) => row.provider === provider.provider,
          );
          return (
            <ProviderRow
              key={provider.provider}
              provider={provider}
              identity={identity}
              loading={loading}
              isLastConnected={Boolean(identity) && rows.length <= 1}
              isMutating={
                startLink.isPending ||
                (unlink.isPending && unlink.variables === provider.provider)
              }
              onConnect={() =>
                startLink.mutate(provider.provider, {
                  onSuccess: (url) => {
                    window.location.href = url;
                  },
                })
              }
              onDisconnect={() =>
                unlink.mutate(provider.provider, {
                  onSuccess: () => {
                    void refreshProfile();
                  },
                })
              }
            />
          );
        })}
      </div>
    </div>
  );
}

function ProviderRow({
  provider,
  identity,
  loading,
  isLastConnected,
  isMutating,
  onConnect,
  onDisconnect,
}: {
  provider: ProviderMeta;
  identity?: AuthIdentity;
  loading: boolean;
  isLastConnected: boolean;
  isMutating: boolean;
  onConnect: () => void;
  onDisconnect: () => void;
}) {
  const { t } = useLocale();
  const connected = Boolean(identity);
  const disabled = loading || isMutating;
  const Icon = provider.Icon;

  return (
    <div className="flex items-center gap-3 rounded-lg px-1 py-3 transition-colors hover:bg-surface-1">
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-surface-2 text-fg-2">
        <Icon className="h-4 w-4" />
      </span>

      <div className="min-w-0 flex-1">
        <p className="text-[15px] text-fg-2">{provider.label}</p>
        <p className="mt-0.5 truncate text-[13px] text-fg-3">
          {loading
            ? ""
            : identity?.provider_email ||
              (connected
                ? t.userSettings.providerConnected
                : t.userSettings.providerNotConnected)}
        </p>
      </div>

      {connected ? (
        <button
          type="button"
          disabled={disabled || isLastConnected}
          onClick={onDisconnect}
          className="rounded-lg px-2.5 py-1.5 text-[13px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-2 disabled:cursor-not-allowed disabled:opacity-45"
        >
          {isLastConnected
            ? t.userSettings.lastProvider
            : t.userSettings.disconnectProvider}
        </button>
      ) : (
        <button
          type="button"
          disabled={disabled}
          onClick={onConnect}
          className="rounded-lg bg-foreground px-2.5 py-1.5 text-[13px] font-medium text-background transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-45"
        >
          {t.userSettings.connectProvider}
        </button>
      )}
    </div>
  );
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 18 18" aria-hidden="true">
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.33-1.58-5.04-3.72H.94v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#FBBC05"
        d="M3.96 10.7A5.41 5.41 0 0 1 3.68 9c0-.59.1-1.16.28-1.7V4.97H.94A9 9 0 0 0 0 9c0 1.45.34 2.82.94 4.03l3.02-2.33Z"
      />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58A8.68 8.68 0 0 0 9 0 9 9 0 0 0 .94 4.97L3.96 7.3C4.67 5.16 6.66 3.58 9 3.58Z"
      />
    </svg>
  );
}
