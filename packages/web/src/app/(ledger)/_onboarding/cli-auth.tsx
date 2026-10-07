"use client";

import { useMemo, useRef, useState, type FormEvent } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Field, Terminal, type TerminalLine } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import {
  DeviceCommandError,
  normalizeUserCode,
  type DeviceRequestView,
} from "@/lib/v2/data/devices";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { setupHref, signInHref } from "@/lib/v2/onboarding/routes";
import { webSessionOf } from "@/lib/v2/web-session";
import { useSource, useViewer } from "../(app)/_lib/data";
import { trackFunnelStep } from "@/lib/v2/funnel";
import { OnboardingPage } from "./frame";
import { useAppHost } from "./setup-bits";
import styles from "./cli-auth.module.css";

type Copy = ReturnType<
  typeof useLocale
>["t"]["ledger"]["onboarding"]["cliAuth"];

/** While a confirmed code waits for the CLI to collect its session. */
const WAIT_FOR_CLI_MS = 2000;

const deviceKeys = {
  code: (kind: string, code: string) => ["v2", kind, "device", code] as const,
};

/** "12 seconds ago", in the page's language. */
function ago(iso: string, now: Date, locale: string): string {
  const seconds = Math.round((new Date(iso).getTime() - now.getTime()) / 1000);
  const rtf = new Intl.RelativeTimeFormat(locale === "zh" ? "zh-CN" : "en", {
    numeric: "auto",
  });
  if (Math.abs(seconds) < 60) return rtf.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return rtf.format(minutes, "minute");
  return rtf.format(Math.round(minutes / 60), "hour");
}

/**
 * CliAuth (CliAuth.png) at /device?code=WQRT-4821: a signed-in person
 * checks the code the memax CLI shows against the terminal and confirms
 * it, or says it doesn't match. The device's own words (its name,
 * system, version, the space it will use) sit beside the address its
 * request came from, which only Memax can vouch for. Confirming needs a
 * session the web app was issued (the API checks it, D15's rule for
 * new credentials); a CLI session here is told to sign in again first.
 * Without a code it asks for one; every ending (signed in, declined,
 * expired, unknown) says what happened and what to do.
 */
export function CliAuthScreen() {
  const { t, locale } = useLocale();
  const copy = t.ledger.onboarding.cliAuth;
  const frame = t.ledger.onboarding.frame;
  const params = useSearchParams();
  const raw = params?.get("code") ?? "";
  const code = normalizeUserCode(raw);
  const viewer = useViewer();
  const source = useSource();
  const now = source.now();

  const meta = viewer
    ? interpolate(frame.signedInAs, { name: viewer.name })
    : null;
  if (!code) {
    return (
      <OnboardingPage meta={meta} width="split" align="center">
        <EnterCode copy={copy} invalid={raw !== ""} />
      </OnboardingPage>
    );
  }
  return (
    <OnboardingPage meta={meta} width="split" align="center">
      <Confirm copy={copy} code={code} now={now} locale={locale} />
    </OnboardingPage>
  );
}

function EnterCode({ copy, invalid }: { copy: Copy; invalid: boolean }) {
  const router = useRouter();
  const pathname = usePathname();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(
    invalid ? copy.enter.invalid : null,
  );
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const code = normalizeUserCode(value);
    if (!code) {
      setError(copy.enter.invalid);
      return;
    }
    router.replace(`${pathname}?code=${encodeURIComponent(code)}`);
  };
  return (
    <section className={styles.enter}>
      <div className={styles.intro}>
        <h1 className={styles.title}>{copy.enter.title}</h1>
        <p className={styles.lede}>{copy.enter.lede}</p>
      </div>
      <form className={styles.enterForm} onSubmit={submit} noValidate>
        <Field
          label={copy.enter.label}
          placeholder="WQRT-4821"
          mono
          autoComplete="one-time-code"
          autoCapitalize="characters"
          spellCheck={false}
          value={value}
          onChange={(e) => setValue(e.currentTarget.value)}
          error={error ?? undefined}
        />
        <Button type="submit" variant="primary" size="lg">
          {copy.enter.submit}
        </Button>
      </form>
      <p className="mx-meta">{copy.note}</p>
    </section>
  );
}

function Confirm({
  copy,
  code,
  now,
  locale,
}: {
  copy: Copy;
  code: string;
  now: Date;
  locale: string;
}) {
  const source = useSource();
  const queryClient = useQueryClient();
  const viewer = useViewer();
  const { user, session } = useAuth();
  const keys = useRef(new IntentKeys());
  const confirmKey = useKeycap("setup.next");
  const [failure, setFailure] = useState<DeviceCommandError | null>(null);
  const key = deviceKeys.code(source.kind, code);

  const lookup = useQuery<DeviceRequestView, DeviceCommandError>({
    queryKey: key,
    queryFn: ({ signal }) => source.devices.lookup({ userCode: code, signal }),
    initialData: () => source.devices.peek?.(code) ?? undefined,
    retry: false,
    refetchInterval: (q) =>
      q.state.data?.state === "approved" ? WAIT_FOR_CLI_MS : false,
  });
  // Only a session the web app was issued can sign a device in (the API
  // decides; this says so before the person tries).
  const web = source.kind === "demo" ? true : webSessionOf(session);
  const decide = useMutation({
    mutationFn: async (to: "approve" | "deny") => {
      const intent = `${to}:${code}`;
      const idempotencyKey = keys.current.keyFor(intent);
      const run =
        to === "approve" ? source.devices.approve : source.devices.deny;
      const result = await run({ userCode: code, idempotencyKey });
      keys.current.settle(intent);
      return result;
    },
    onSuccess: (result, to) => {
      setFailure(null);
      queryClient.setQueryData(key, result);
      if (to === "approve" && source.kind !== "demo") {
        trackFunnelStep("device_approved");
      }
    },
    onError: (err) =>
      setFailure(
        err instanceof DeviceCommandError
          ? err
          : new DeviceCommandError("failed"),
      ),
  });

  const request = lookup.data;
  const pending = request?.state === "pending" && web !== false;
  useHotkey("setup.next", () => decide.mutate("approve"), {
    enabled: pending && !decide.isPending,
  });

  const host = useAppHost();
  const lines = useMemo<TerminalLine[]>(() => {
    const out: TerminalLine[] = [];
    if (request?.clientVersion) {
      out.push({
        kind: "dim",
        text: interpolate(copy.terminal.version, {
          version: request.clientVersion,
        }),
      });
      out.push({ kind: "blank" });
    }
    out.push({ kind: "out", text: interpolate(copy.terminal.open, { host }) });
    out.push({ kind: "blank" });
    out.push({ kind: "out", text: `    ${code}` });
    out.push({ kind: "blank" });
    if (request?.state === "signed_in" && viewer) {
      out.push({
        kind: "ok",
        text: interpolate(copy.terminal.signedIn, { name: viewer.name }),
      });
    } else {
      out.push({ kind: "dim", text: copy.terminal.waiting });
    }
    return out;
  }, [copy, code, request, viewer, host]);

  if (lookup.isPending) {
    return (
      <p className="mx-sr" role="status">
        {copy.title}
      </p>
    );
  }
  const error = failure ?? (lookup.isError ? lookup.error : null);
  const ended = request ? endedCopy(copy, request) : null;
  const notFound = !request && error?.refusal === "not_found";

  return (
    <div className={styles.split}>
      <section className={styles.left} aria-labelledby="cli-auth-title">
        <div className={styles.intro}>
          <h1 className={styles.title} id="cli-auth-title">
            {ended?.title ?? (notFound ? copy.notFound.title : copy.title)}
          </h1>
          <p className={styles.lede}>
            {ended?.lede ?? (notFound ? copy.notFound.lede : copy.lede)}
          </p>
        </div>
        <CodeBoxes code={code} label={interpolate(copy.codeLabel, { code })} />
        {request ? (
          <dl className={styles.facts}>
            <dt>{copy.device}</dt>
            <dd>
              {request.deviceName || request.deviceOs
                ? interpolate(copy.deviceValue, {
                    name: request.deviceName ?? copy.unknown,
                    os: request.deviceOs ?? copy.unknown,
                  })
                : copy.unknown}
            </dd>
            <dt>{copy.cli}</dt>
            <dd>
              {request.clientVersion
                ? interpolate(copy.cliValue, { version: request.clientVersion })
                : request.clientId}
            </dd>
            <dt>{copy.asked}</dt>
            <dd>
              {interpolate(copy.askedValue, {
                when: ago(request.requestedAt, now, locale),
                address: request.address ?? copy.unknown,
              })}
            </dd>
            <dt>{copy.willUse}</dt>
            <dd>
              {request.space
                ? interpolate(copy.willUseValue, { space: request.space })
                : copy.willUseChosen}
            </dd>
          </dl>
        ) : null}
        {request?.state === "pending" && web === false ? (
          <div className={styles.notice} role="note">
            <strong>{copy.needsWeb.title}</strong>
            <span>{copy.needsWeb.lede}</span>
          </div>
        ) : null}
        {error && !notFound ? (
          <p className={styles.error} role="alert">
            {error.refusal === "needs_web"
              ? copy.needsWeb.lede
              : error.refusal === "rate_limited"
                ? interpolate(copy.rateLimited, {
                    n: Math.max(
                      1,
                      Math.ceil((error.detail.retryAfter ?? 600) / 60),
                    ),
                  })
                : error.refusal === "decided" && error.detail.state
                  ? endedCopy(copy, { state: error.detail.state })?.title
                  : copy.failed}
          </p>
        ) : null}
        <Actions
          copy={copy}
          request={request ?? null}
          web={web}
          confirmKey={confirmKey}
          busy={decide.isPending}
          onDecide={(to) => decide.mutate(to)}
          next={user ? setupHref("import", { space: request?.space }) : null}
          code={code}
        />
        <p className="mx-meta">{copy.note}</p>
      </section>
      <Terminal
        title={interpolate(copy.terminalTitle, {
          device: request?.deviceName ?? "memax",
        })}
        lines={lines}
      />
    </div>
  );
}

function endedCopy(
  copy: Copy,
  request: Pick<DeviceRequestView, "state">,
): { title: string; lede: string } | null {
  switch (request.state) {
    case "approved":
      return copy.approved;
    case "signed_in":
      return copy.signedIn;
    case "denied":
      return copy.denied;
    case "expired":
      return copy.expired;
    default:
      return null;
  }
}

function Actions({
  copy,
  request,
  web,
  confirmKey,
  busy,
  onDecide,
  next,
  code,
}: {
  copy: Copy;
  request: DeviceRequestView | null;
  web: boolean | null;
  confirmKey: string;
  busy: boolean;
  onDecide: (to: "approve" | "deny") => void;
  next: string | null;
  code: string;
}) {
  if (!request) return null;
  if (request.state === "signed_in") {
    return next ? (
      <div className="mx-inline">
        <Button variant="primary" size="lg" href={next} icon="arrow-right">
          {copy.signedIn.follow}
        </Button>
      </div>
    ) : null;
  }
  if (request.state !== "pending") return null;
  if (web === false) {
    return (
      <div className="mx-inline">
        <Button
          variant="primary"
          size="lg"
          href={signInHref(`/device?code=${encodeURIComponent(code)}`, {
            again: true,
          })}
        >
          {copy.needsWeb.action}
        </Button>
      </div>
    );
  }
  return (
    <div className="mx-inline">
      <Button
        variant="primary"
        size="lg"
        kbd={confirmKey}
        pending={busy}
        onClick={() => onDecide("approve")}
      >
        {copy.confirm}
      </Button>
      <Button size="lg" disabled={busy} onClick={() => onDecide("deny")}>
        {copy.mismatch}
      </Button>
    </div>
  );
}

/** The code in boxes, as the terminal shows it: four letters, a dash, four digits. */
function CodeBoxes({ code, label }: { code: string; label: string }) {
  const [letters, digits] = code.split("-");
  return (
    <div className={styles.code} role="group" aria-label={label}>
      {[...(letters ?? "")].map((c, i) => (
        <span key={`l${i}`} className={styles.char} aria-hidden="true">
          {c}
        </span>
      ))}
      <span className={styles.dash} aria-hidden="true" />
      {[...(digits ?? "")].map((c, i) => (
        <span key={`d${i}`} className={styles.char} aria-hidden="true">
          {c}
        </span>
      ))}
    </div>
  );
}
