"use client";

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Dialog } from "@base-ui/react/dialog";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Field, Segmented } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { toFailure, type CommandFailure } from "@/lib/v2/data/command-error";
import type { SpaceKind, SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { spaceFailureText } from "@/lib/v2/records-copy";
import {
  isRepository,
  MAX_SPACE_NAME,
  MAX_SPACE_REPOSITORY,
  nameOfRepository,
} from "@/lib/v2/space-name";
import { ledgerQueryKeys, useSource, useSpaces } from "../_lib/data";
import { useOverlays } from "../_lib/overlays";
import { useToast } from "./toasts";
import styles from "./new-space.module.css";

/** `?overlay=new-space` opens it (plan §6.3: overlays deep-link). */
const OVERLAY = "new-space";

const KINDS: readonly SpaceKind[] = ["personal", "project", "team"];

type StartFrom = "agents" | "conventions" | "nothing";

/**
 * New space (NewSpace.png), the layer the space switcher opens: the
 * kind, the name and repository, who it belongs to and what it starts
 * from, then Create space (↵), which makes it through the API and lands
 * on its Today, the empty space's three steps (EmptySpace.png).
 *
 * The API makes project spaces. Each person has one personal space, made
 * with their account, and team spaces come with Team: choosing either
 * says so before anything is sent, and if the API is asked anyway its
 * refusal is worded by records-copy (personal_exists, space_kind). What
 * agents already know is read by `memax init` in the repository, the
 * empty space's first step; copying another space's conventions isn't
 * built yet and says so.
 *
 * Base UI's Dialog brings the focus trap, Escape, outside press, scroll
 * lock and focus return; a modal key scope silences the page's keys.
 */
export function NewSpaceDialog() {
  const { newSpace, openNewSpace, closeNewSpace } = useOverlays();
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const query = params?.toString() ?? "";
  const deepLinked = params?.get("overlay") === OVERLAY;
  const opened = useRef(false);
  // The opening it shows, kept while it closes so the closing layer is the
  // one that was open.
  const [session, setSession] = useState(0);
  if (newSpace !== null && newSpace !== session) setSession(newSpace);

  // ?overlay=new-space opens it once, then leaves the address.
  useEffect(() => {
    if (!deepLinked) {
      opened.current = false;
      return;
    }
    if (opened.current || !pathname) return;
    opened.current = true;
    openNewSpace();
    const next = new URLSearchParams(query);
    next.delete("overlay");
    const rest = next.toString();
    router.replace(rest ? `${pathname}?${rest}` : pathname, { scroll: false });
  }, [deepLinked, openNewSpace, query, pathname, router]);

  return (
    <Dialog.Root
      open={newSpace !== null}
      onOpenChange={(open) => {
        if (!open) closeNewSpace();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {/* Keyed by session: each opening starts from the defaults. */}
        <NewSpaceForm key={session} onClose={closeNewSpace} />
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function NewSpaceForm({ onClose }: { onClose: () => void }) {
  const { t, locale } = useLocale();
  const n = t.ledger.newSpace;
  const source = useSource();
  const queryClient = useQueryClient();
  const router = useRouter();
  const toast = useToast();
  const spaces = useSpaces().data ?? [];
  const personal = spaces.find((s) => s.kind === "personal");
  const teams = spaces.filter((s) => s.kind === "team" && s.onV2 !== false);
  // Conventions could come from another project space with something kept.
  const other = spaces.find(
    (s) => s.kind === "project" && s.onV2 !== false && (s.kept ?? 0) > 0,
  );

  const [kind, setKind] = useState<SpaceKind>("project");
  const [name, setName] = useState("");
  const [named, setNamed] = useState(false);
  const [repository, setRepository] = useState("");
  const [owner, setOwner] = useState("me");
  const [start, setStart] = useState<StartFrom>("agents");
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<CommandFailure | null>(null);
  const [tried, setTried] = useState(false);
  const keys = useMemo(() => new IntentKeys(), []);
  const kindRefs = useRef(new Map<SpaceKind, HTMLButtonElement>());

  const trimmed = name.trim();
  const repo = repository.trim();
  const nameError = !trimmed
    ? n.nameRequired
    : [...trimmed].length > MAX_SPACE_NAME
      ? n.nameTooLong
      : null;
  const repoError = isRepository(repo) ? null : n.repositoryInvalid;
  // What the API would refuse, said before anything is sent.
  const blocked =
    kind === "personal"
      ? personal
        ? interpolate(n.personalExists, { space: personal.name })
        : null
      : kind === "team"
        ? n.teamLater
        : owner !== "me"
          ? n.teamOwnerLater
          : null;

  const chooseKind = (next: SpaceKind) => {
    setKind(next);
    setFailure(null);
  };
  // The kinds are a radio group: one tab stop, arrows choose.
  const onKindKeys = (event: KeyboardEvent<HTMLDivElement>) => {
    const step =
      event.key === "ArrowRight" || event.key === "ArrowDown"
        ? 1
        : event.key === "ArrowLeft" || event.key === "ArrowUp"
          ? -1
          : 0;
    if (!step) return;
    event.preventDefault();
    const next =
      KINDS[(KINDS.indexOf(kind) + step + KINDS.length) % KINDS.length];
    chooseKind(next);
    kindRefs.current.get(next)?.focus();
  };

  const onRepository = (value: string) => {
    setRepository(value);
    setFailure(null);
    // The name follows the repository until the person names it.
    if (!named) setName(nameOfRepository(value));
  };

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    setTried(true);
    if (pending || blocked || nameError || repoError) return;
    const intent = intentOf("create-space", "", 0, kind, trimmed, repo);
    setPending(true);
    setFailure(null);
    try {
      const space = await source.createSpace({
        name: trimmed,
        kind,
        repository: repo,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      // The switcher lists it at once; the next read confirms it.
      queryClient.setQueryData<SpaceSummary[]>(
        ledgerQueryKeys.spaces(source.kind),
        (list) =>
          list && !list.some((s) => s.id === space.id)
            ? [...list, space]
            : list,
      );
      void queryClient.invalidateQueries({
        queryKey: ledgerQueryKeys.spaces(source.kind),
      });
      toast({ text: interpolate(n.created, { space: space.name }) });
      onClose();
      router.push(placeHref(space.slug, "today"));
    } catch (err) {
      const f = toFailure(err);
      // A refusal is the answer; a dropped request may be retried as is.
      if (f.kind === "refused" || f.kind === "unknown") keys.settle(intent);
      setFailure(f);
    } finally {
      setPending(false);
    }
  };

  const problem = failure
    ? spaceFailureText(t.ledger.records, failure, {
        name: trimmed || n.title,
        locale,
      })
    : null;
  const showNameError = tried && kind === "project" && nameError;
  const startOptions: {
    value: StartFrom;
    title: string;
    detail?: string;
    disabled?: string;
  }[] = [
    {
      value: "agents",
      title: repo ? n.start.agents.title : n.start.agentsAnywhere.title,
      detail: repo ? n.start.agents.detail : n.start.agentsAnywhere.detail,
    },
    ...(other
      ? [
          {
            value: "conventions" as const,
            title: interpolate(n.start.conventions.title, {
              space: other.name,
            }),
            detail: n.start.conventions.detail,
            disabled: n.conventionsLater,
          },
        ]
      : []),
    { value: "nothing", title: n.start.nothing },
  ];

  return (
    <Dialog.Popup className={styles.layer} aria-labelledby="new-space-title">
      <KeyScopeBoundary name="new-space" modal>
        <form className={styles.dialog} onSubmit={submit} noValidate>
          <header className={styles.head}>
            <Dialog.Title id="new-space-title" className={styles.title}>
              {n.title}
            </Dialog.Title>
            <Dialog.Close
              render={
                <Button
                  type="button"
                  variant="quiet"
                  size="sm"
                  icon="x"
                  aria-label={n.close}
                />
              }
            />
          </header>

          <div className={styles.body}>
            <div>
              <p className={styles.label} id="new-space-kind">
                {n.kind}
              </p>
              <div
                role="radiogroup"
                aria-label={n.kindLabel}
                className={styles.kinds}
                onKeyDown={onKindKeys}
              >
                {KINDS.map((k) => (
                  <button
                    key={k}
                    ref={(el) => {
                      if (el) kindRefs.current.set(k, el);
                      else kindRefs.current.delete(k);
                    }}
                    type="button"
                    role="radio"
                    aria-checked={kind === k}
                    tabIndex={kind === k ? 0 : -1}
                    className={styles.kind}
                    onClick={() => chooseKind(k)}
                  >
                    <b>{n.kinds[k].title}</b>
                    <span>{n.kinds[k].detail}</span>
                  </button>
                ))}
              </div>
              {blocked && kind !== "project" ? (
                <p className={styles.note} role="status">
                  <span>{blocked}</span>
                  {kind === "personal" && personal ? (
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        onClose();
                        router.push(placeHref(personal.slug, "today"));
                      }}
                    >
                      {interpolate(n.openPersonal, { space: personal.name })}
                    </Button>
                  ) : null}
                </p>
              ) : null}
            </div>

            <div className={styles.pair}>
              <Field
                label={n.name}
                mono
                value={name}
                maxLength={MAX_SPACE_NAME}
                autoComplete="off"
                spellCheck={false}
                error={showNameError || undefined}
                onChange={(event) => {
                  setName(event.target.value);
                  setNamed(event.target.value.trim() !== "");
                  setFailure(null);
                }}
              />
              <Field
                label={n.repository}
                mono
                value={repository}
                placeholder={n.repositoryPlaceholder}
                maxLength={MAX_SPACE_REPOSITORY}
                autoComplete="off"
                spellCheck={false}
                error={repoError ?? undefined}
                onChange={(event) => onRepository(event.target.value)}
              />
            </div>

            {teams.length > 0 ? (
              <div>
                <p className={styles.label}>{n.belongsTo}</p>
                <Segmented
                  size="sm"
                  label={n.belongsTo}
                  value={owner}
                  onChange={(value) => {
                    setOwner(value);
                    setFailure(null);
                  }}
                  options={[
                    { value: "me", label: n.justMe },
                    ...teams.map((team) => ({
                      value: team.id,
                      label: team.name,
                      disabled: true,
                      disabledReason: n.teamOwnerLater,
                    })),
                  ]}
                />
              </div>
            ) : null}

            <fieldset className={styles.starts}>
              <legend className={styles.label}>{n.startFrom}</legend>
              {startOptions.map((option) => (
                <label
                  key={option.value}
                  className={`${styles.start} ${start === option.value ? styles.on : ""} ${option.disabled ? styles.off : ""}`}
                  title={option.disabled}
                >
                  <input
                    type="radio"
                    name="new-space-start"
                    value={option.value}
                    checked={start === option.value}
                    disabled={Boolean(option.disabled)}
                    onChange={() => setStart(option.value)}
                  />
                  <span className={styles.startText}>
                    <span className={styles.startTitle}>{option.title}</span>
                    {option.detail ? (
                      <span className="mx-meta">{option.detail}</span>
                    ) : null}
                  </span>
                </label>
              ))}
            </fieldset>

            {problem ? (
              <p className={styles.problem} role="alert">
                {problem}
              </p>
            ) : null}
          </div>

          <footer className={styles.foot}>
            <span className={`mx-meta ${styles.footNote}`}>{n.foot}</span>
            <Dialog.Close
              render={
                <Button type="button" variant="quiet">
                  {n.cancel}
                </Button>
              }
            />
            <Button
              type="submit"
              variant="primary"
              kbd="↵"
              pending={pending}
              disabled={Boolean(blocked)}
              disabledReason={blocked ?? undefined}
            >
              {n.create}
            </Button>
          </footer>
        </form>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}
