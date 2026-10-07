"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { getMemaxClient, getPublicMemaxClient } from "@/lib/memax-client";
import { pluralize, useInterpolate, useLocale } from "@/i18n";
import { MemaxLoader, MemaxWordmark } from "@memaxlabs/ui";
import type { InviteDetails } from "memax-sdk";

type InviteState =
  | { status: "loading" }
  | { status: "ready"; data: InviteDetails }
  | { status: "error"; code: string; message: string }
  | { status: "accepted" };

function getErrorCode(error: unknown): string {
  if (error && typeof error === "object" && "code" in error) {
    return String((error as { code?: unknown }).code ?? "unknown");
  }
  return "unknown";
}

function getErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

export default function InviteAcceptPage() {
  const { token } = useParams<{ token: string }>();
  const {
    user,
    loading: authLoading,
    login,
    refreshProfile,
    switchHub,
  } = useAuth();
  const router = useRouter();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const [state, setState] = useState<InviteState>({ status: "loading" });
  const [accepting, setAccepting] = useState(false);
  const autoAcceptRef = useRef(false);

  useEffect(() => {
    if (!token) return;
    getPublicMemaxClient()
      .invites.get(token)
      .then((data) => {
        setState({ status: "ready", data });
      })
      .catch((error) => {
        setState({
          status: "error",
          code: getErrorCode(error),
          message: getErrorMessage(error, t.hubs.loadInviteFailed),
        });
      });
  }, [t.hubs.loadInviteFailed, token]);

  const finishJoin = useCallback(
    async (hubName: string, hubId: string, message: string) => {
      await refreshProfile();
      switchHub(hubId);
      localStorage.setItem(
        "memax_pending_notif",
        JSON.stringify({ type: "success", message }),
      );
      setState({ status: "accepted" });
      setTimeout(() => router.push("/home"), 1200);
    },
    [refreshProfile, router, switchHub],
  );

  const handleAccept = useCallback(async () => {
    if (!user || accepting || state.status !== "ready") return;
    setAccepting(true);
    try {
      try {
        await getMemaxClient().invites.accept(token);
      } catch (error) {
        const code = getErrorCode(error);
        if (code === "already_member") {
          await finishJoin(
            state.data.hub.name,
            state.data.hub.id,
            t.hubs.switchedTo.replace("{name}", state.data.hub.name),
          );
          return;
        }
        setState({
          status: "error",
          code,
          message: getErrorMessage(error, t.hubs.acceptInviteFailed),
        });
        setAccepting(false);
        return;
      }

      await finishJoin(
        state.data.hub.name,
        state.data.hub.id,
        t.hubs.joinedHub.replace("{name}", state.data.hub.name),
      );
    } catch {
      setAccepting(false);
    }
  }, [
    accepting,
    finishJoin,
    state,
    t.hubs.acceptInviteFailed,
    t.hubs.joinedHub,
    t.hubs.switchedTo,
    token,
    user,
  ]);

  useEffect(() => {
    if (
      user &&
      state.status === "ready" &&
      !accepting &&
      !autoAcceptRef.current
    ) {
      const returnTo = localStorage.getItem("memax_return_to");
      if (returnTo?.includes("/invite/")) {
        localStorage.removeItem("memax_return_to");
        autoAcceptRef.current = true;
        void handleAccept();
      }
    }
  }, [accepting, handleAccept, state.status, user]);

  if (authLoading || state.status === "loading") {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <MemaxLoader />
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div
        className="w-full max-w-[320px] rounded-surface px-8 py-10 text-center"
        style={{
          background: "var(--card)",
          border: "1px solid var(--border)",
          boxShadow: "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
        }}
      >
        <MemaxWordmark height={18} className="text-foreground mx-auto mb-6" />

        {state.status === "error" && (
          <>
            <p className="text-[18px] font-bold text-foreground mb-4">
              {state.code === "expired"
                ? t.hubs.inviteExpired
                : state.code === "used"
                  ? t.hubs.inviteUsed
                  : t.hubs.inviteNotFound}
            </p>
            <p className="text-[13px] text-foreground/40">{state.message}</p>
          </>
        )}

        {state.status === "accepted" && (
          <>
            <p className="text-[18px] font-bold text-foreground mb-2">
              {t.hubs.joined}
            </p>
            <p className="text-[13px] text-foreground/40">
              {t.hubs.redirecting}
            </p>
          </>
        )}

        {state.status === "ready" && (
          <>
            <p
              className="text-[19px] font-bold text-foreground mb-4"
              style={{ letterSpacing: "-0.02em" }}
            >
              {t.hubs.joinHub.replace("{name}", state.data.hub.name)}
            </p>

            <div className="text-[13px] text-foreground/45 space-y-1 mb-6">
              <p>
                {pluralize(
                  t.hubs.memberOne,
                  t.hubs.members,
                  state.data.member_count,
                )}
              </p>
              {state.data.invited_by && (
                <p>
                  {t.hubs.invitedBy.replace("{name}", state.data.invited_by)}
                </p>
              )}
            </div>

            <p className="text-[12px] text-foreground/30 mb-6">
              {t.hubs.joinDesc}
            </p>

            {user ? (
              <button
                onClick={() => void handleAccept()}
                disabled={accepting}
                className="w-full py-2.5 rounded-surface text-[14px] font-semibold text-background bg-foreground hover:bg-foreground/90 transition-colors cursor-pointer disabled:opacity-50"
              >
                {accepting
                  ? "..."
                  : t.hubs.joinButton.replace("{name}", state.data.hub.name)}
              </button>
            ) : (
              <button
                onClick={() => login(`/invite/${token}`)}
                className="w-full py-2.5 rounded-surface text-[14px] font-semibold text-background bg-foreground hover:bg-foreground/90 transition-colors cursor-pointer"
              >
                {t.hubs.loginToJoin}
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
}
