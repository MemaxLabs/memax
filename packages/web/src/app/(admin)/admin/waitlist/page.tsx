"use client";

import { useState, useCallback, useMemo } from "react";
import Link from "next/link";
import { useLocale } from "@/i18n";
import {
  Button,
  Badge,
  Skeleton,
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@memaxlabs/ui";
import {
  useAdminWaitlist,
  useAdminWaitlistStats,
  useApproveWaitlistEntry,
  useRejectWaitlistEntry,
  useBatchApproveWaitlist,
  useInviteByEmail,
  useRevokeInvite,
  useRestoreWaitlistEntry,
} from "@/hooks/use-admin-waitlist";
import type { WaitlistEntry } from "@/hooks/use-admin-waitlist";
import { useAdminCursor } from "@/hooks/use-admin-cursor";
import { OrphanBanner } from "@/components/features/admin/waitlist/orphan-banner";
import { AdminListFooter } from "@/components/features/admin/admin-list-footer";
import {
  Check,
  X,
  Search,
  Send,
  Ban,
  Eye,
  UserPlus,
  RotateCcw,
} from "lucide-react";

export default function WaitlistPage() {
  const { t } = useLocale();
  const [statusFilter, setStatusFilter] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [batchWave, setBatchWave] = useState(1);
  const { cursor, hasPrev, goForward, goBack, reset } = useAdminCursor();

  const { data: stats, isLoading: statsLoading } = useAdminWaitlistStats();
  const { data: listData, isLoading: listLoading } = useAdminWaitlist({
    status: statusFilter || undefined,
    q: searchQuery || undefined,
    limit: 50,
    cursor,
  });

  const approveMutation = useApproveWaitlistEntry();
  const rejectMutation = useRejectWaitlistEntry();
  const batchApproveMutation = useBatchApproveWaitlist();
  const revokeInviteMutation = useRevokeInvite();
  const restoreMutation = useRestoreWaitlistEntry();

  const entries = useMemo(() => listData?.entries ?? [], [listData?.entries]);
  const allSelected =
    entries.length > 0 && entries.every((e) => selectedIds.has(e.id));
  const someSelected = selectedIds.size > 0;

  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleSelectAll = useCallback(() => {
    if (allSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(entries.map((e) => e.id)));
    }
  }, [allSelected, entries]);

  const handleBatchApprove = useCallback(() => {
    const ids = Array.from(selectedIds);
    batchApproveMutation.mutate(
      { ids, wave: batchWave },
      { onSuccess: () => setSelectedIds(new Set()) },
    );
  }, [selectedIds, batchApproveMutation, batchWave]);

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-6xl">
      {/* Header */}
      <div className="mb-8 flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-fg-1">
            {t.admin.waitlist.title}
          </h1>
          <p className="mt-1 text-[14px] text-fg-3">
            {t.admin.waitlist.description}
          </p>
        </div>
        <InviteByEmailDialog />
      </div>

      {/* Orphan-invite repair pointer — renders nothing when the
          cohort is empty, so this slot is invisible unless there
          is actionable work. */}
      <OrphanBanner />

      {/* Stats cards */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 mb-8">
        {statsLoading ? (
          Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="rounded-surface border border-border/50 bg-card px-4 py-3"
            >
              <Skeleton className="h-3 w-12 mb-2" />
              <Skeleton className="h-6 w-8" />
            </div>
          ))
        ) : (
          <>
            <StatCard
              label={t.admin.waitlist.stats.total}
              value={stats?.total ?? 0}
            />
            <StatCard
              label={t.admin.waitlist.stats.pending}
              value={stats?.pending ?? 0}
              accent
            />
            <StatCard
              label={t.admin.waitlist.stats.approved}
              value={stats?.approved ?? 0}
            />
            <StatCard
              label={t.admin.waitlist.stats.registered}
              value={stats?.registered ?? 0}
            />
            <StatCard
              label={t.admin.waitlist.stats.rejected}
              value={stats?.rejected ?? 0}
            />
            <CapacityCard
              label={t.admin.waitlist.stats.capacity}
              used={stats?.capacity_used ?? 0}
              cap={stats?.capacity_cap ?? 0}
            />
          </>
        )}
      </div>

      {/* Toolbar: search + filter + batch actions */}
      <div className="flex items-center gap-3 mb-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-fg-4" />
          <input
            type="text"
            placeholder={t.admin.waitlist.search}
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              reset();
            }}
            className="w-full rounded-lg border border-border/50 bg-card pl-9 pr-3 py-2 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
          />
        </div>

        <Select
          value={statusFilter}
          onValueChange={(v: string) => {
            setStatusFilter(v);
            reset();
          }}
          items={{
            "": t.admin.waitlist.allStatuses,
            pending: t.admin.waitlist.status.pending,
            approved: t.admin.waitlist.status.approved,
            registered: t.admin.waitlist.status.registered,
            rejected: t.admin.waitlist.status.rejected,
          }}
        >
          <SelectTrigger
            placeholder={t.admin.waitlist.allStatuses}
            className="w-44"
          />
          <SelectContent>
            <SelectItem value="">{t.admin.waitlist.allStatuses}</SelectItem>
            <SelectItem value="pending">
              {t.admin.waitlist.status.pending}
            </SelectItem>
            <SelectItem value="approved">
              {t.admin.waitlist.status.approved}
            </SelectItem>
            <SelectItem value="registered">
              {t.admin.waitlist.status.registered}
            </SelectItem>
            <SelectItem value="rejected">
              {t.admin.waitlist.status.rejected}
            </SelectItem>
          </SelectContent>
        </Select>

        {someSelected && (
          <>
            <div className="flex items-center gap-1.5">
              <label className="text-[12px] text-fg-3">
                {t.admin.waitlist.wave}
              </label>
              <input
                type="number"
                min={1}
                value={batchWave}
                onChange={(e) =>
                  setBatchWave(Math.max(1, parseInt(e.target.value) || 1))
                }
                className="w-14 rounded-lg border border-border/50 bg-card px-2 py-1.5 text-[13px] text-fg-1 text-center outline-none focus:border-fg-3 tabular-nums"
              />
            </div>
            <Button
              size="sm"
              onClick={handleBatchApprove}
              disabled={batchApproveMutation.isPending}
            >
              <Check className="h-3.5 w-3.5 mr-1" />
              {t.admin.waitlist.actions.approveSelected.replace(
                "{count}",
                String(selectedIds.size),
              )}
            </Button>
          </>
        )}
      </div>

      {/* Table */}
      <div className="rounded-surface border border-border/50 bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-[14px] min-w-[640px]">
            <thead>
              <tr className="border-b border-border/30 text-fg-3 text-left">
                <th className="px-4 py-3 w-10">
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={toggleSelectAll}
                    className="rounded cursor-pointer"
                  />
                </th>
                <th className="px-4 py-3 font-medium">
                  {t.admin.waitlist.table.email}
                </th>
                <th className="px-4 py-3 font-medium hidden lg:table-cell">
                  {t.admin.waitlist.table.useCase}
                </th>
                <th className="px-4 py-3 font-medium hidden xl:table-cell">
                  {t.admin.waitlist.table.aiTools}
                </th>
                <th className="px-4 py-3 font-medium hidden xl:table-cell">
                  {t.admin.waitlist.table.role}
                </th>
                <th className="px-4 py-3 font-medium">
                  {t.admin.waitlist.table.status}
                </th>
                <th className="px-4 py-3 font-medium hidden lg:table-cell">
                  {t.admin.waitlist.table.invite}
                </th>
                <th className="px-4 py-3 font-medium hidden md:table-cell">
                  {t.admin.waitlist.table.signedUp}
                </th>
                <th className="px-4 py-3 font-medium w-28">
                  {t.admin.waitlist.table.actions}
                </th>
              </tr>
            </thead>
            <tbody>
              {listLoading
                ? Array.from({ length: 8 }).map((_, i) => (
                    <tr key={i} className="border-b border-border/20">
                      <td className="px-4 py-3">
                        <Skeleton className="h-4 w-4" />
                      </td>
                      <td className="px-4 py-3">
                        <Skeleton className="h-4 w-40" />
                      </td>
                      <td className="px-4 py-3 hidden lg:table-cell">
                        <Skeleton className="h-4 w-24" />
                      </td>
                      <td className="px-4 py-3 hidden xl:table-cell">
                        <Skeleton className="h-4 w-32" />
                      </td>
                      <td className="px-4 py-3 hidden xl:table-cell">
                        <Skeleton className="h-4 w-20" />
                      </td>
                      <td className="px-4 py-3">
                        <Skeleton className="h-5 w-16" />
                      </td>
                      <td className="px-4 py-3 hidden lg:table-cell">
                        <Skeleton className="h-4 w-14" />
                      </td>
                      <td className="px-4 py-3 hidden md:table-cell">
                        <Skeleton className="h-4 w-20" />
                      </td>
                      <td className="px-4 py-3">
                        <Skeleton className="h-7 w-20" />
                      </td>
                    </tr>
                  ))
                : entries.map((entry) => (
                    <WaitlistRow
                      key={entry.id}
                      entry={entry}
                      selected={selectedIds.has(entry.id)}
                      onSelect={toggleSelect}
                      onApprove={(id) => approveMutation.mutate({ id })}
                      onReject={(id) => rejectMutation.mutate({ id })}
                      onRevokeInvite={(id) => revokeInviteMutation.mutate(id)}
                      onRestore={(id) => restoreMutation.mutate(id)}
                      approving={approveMutation.isPending}
                    />
                  ))}
            </tbody>
          </table>
        </div>

        {!listLoading && entries.length === 0 && (
          <div className="px-4 py-12 text-center text-fg-3 text-[14px]">
            {t.admin.waitlist.empty}
          </div>
        )}

        {listData && (
          <AdminListFooter
            total={listData.total_count}
            hasPrev={hasPrev}
            hasNext={!!listData.next_cursor}
            onPrev={goBack}
            onNext={() => goForward(listData.next_cursor)}
          />
        )}
      </div>
    </div>
  );
}

// --- Invite by Email Dialog ---

function InviteByEmailDialog() {
  const { t } = useLocale();
  const [email, setEmail] = useState("");
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const inviteByEmailMutation = useInviteByEmail();

  const handleSend = () => {
    if (!email) return;
    setError("");
    inviteByEmailMutation.mutate(
      { email },
      {
        onSuccess: () => {
          setEmail("");
          setError("");
          setOpen(false);
        },
        onError: (err: unknown) => {
          const msg =
            err instanceof Error ? err.message : t.states.error.unexpected;
          setError(msg);
        },
      },
    );
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setEmail("");
      setError("");
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button size="sm" variant="outline">
            <UserPlus className="h-3.5 w-3.5 mr-1.5" />
            {t.admin.waitlist.actions.inviteByEmail}
          </Button>
        }
      />
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t.admin.waitlist.actions.inviteByEmail}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div>
            <label className="block text-[12px] font-medium text-fg-3 mb-1.5">
              {t.admin.waitlist.table.email}
            </label>
            <div className="relative">
              <Send className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-fg-4" />
              <input
                type="email"
                autoFocus
                placeholder={t.admin.waitlist.actions.inviteEmailPlaceholder}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleSend();
                }}
                className="w-full rounded-surface border border-border/50 bg-background pl-9 pr-3 py-2.5 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
              />
            </div>
          </div>
          {error && <p className="text-[13px] text-red-500">{error}</p>}
          <div className="flex justify-end">
            <Button
              size="sm"
              disabled={!email || inviteByEmailMutation.isPending}
              onClick={handleSend}
            >
              {inviteByEmailMutation.isPending ? (
                <span className="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
              ) : (
                <>
                  <Send className="h-3.5 w-3.5 mr-1.5" />
                  {t.admin.waitlist.actions.sendInvite}
                </>
              )}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// --- Entry Detail Dialog ---

function EntryDetailDialog({ entry }: { entry: WaitlistEntry }) {
  const { t, locale } = useLocale();

  const statusLabel =
    t.admin.waitlist.status[
      entry.status as keyof typeof t.admin.waitlist.status
    ] ?? entry.status;

  const useCaseLabel =
    t.admin.waitlist.useCase[
      entry.use_case as keyof typeof t.admin.waitlist.useCase
    ] ?? entry.use_case;

  const roleLabel = entry.role
    ? (t.waitlistPage.roleOptions[
        entry.role as keyof typeof t.waitlistPage.roleOptions
      ] ?? entry.role)
    : t.admin.waitlist.detail.none;

  const inviteLabel = entry.invite_status
    ? (t.admin.waitlist.inviteStatus[
        entry.invite_status as keyof typeof t.admin.waitlist.inviteStatus
      ] ?? entry.invite_status)
    : t.admin.waitlist.detail.none;

  return (
    <Dialog>
      <DialogTrigger
        render={
          <Button
            size="xs"
            variant="ghost"
            title={t.admin.waitlist.actions.viewDetails}
          >
            <Eye className="h-3.5 w-3.5" />
          </Button>
        }
      />
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t.admin.waitlist.detail.title}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <DetailRow label={t.admin.waitlist.detail.email}>
            <span className="text-fg-1 font-medium">{entry.email}</span>
          </DetailRow>
          <DetailRow label={t.admin.waitlist.detail.status}>
            <Badge
              variant={
                entry.status === "pending"
                  ? "outline"
                  : entry.status === "approved"
                    ? "secondary"
                    : entry.status === "registered"
                      ? "default"
                      : "destructive"
              }
            >
              {statusLabel}
            </Badge>
          </DetailRow>
          <DetailRow label={t.admin.waitlist.detail.useCase}>
            {useCaseLabel || t.admin.waitlist.detail.none}
          </DetailRow>
          <DetailRow label={t.admin.waitlist.detail.aiTools}>
            {entry.ai_tools?.length ? (
              <div className="flex flex-wrap gap-1">
                {entry.ai_tools.map((tool) => (
                  <span
                    key={tool}
                    className="inline-block px-1.5 py-0.5 rounded text-[11px] bg-surface-1 text-fg-3"
                  >
                    {tool}
                  </span>
                ))}
              </div>
            ) : (
              t.admin.waitlist.detail.none
            )}
          </DetailRow>
          <DetailRow label={t.admin.waitlist.detail.role}>
            {roleLabel}
          </DetailRow>
          <DetailRow label={t.admin.waitlist.detail.invite}>
            {inviteLabel}
          </DetailRow>
          {entry.wave != null && (
            <DetailRow label={t.admin.waitlist.detail.wave}>
              {entry.wave}
            </DetailRow>
          )}
          <DetailRow label={t.admin.waitlist.detail.signedUp}>
            {formatDate(entry.created_at, locale)}
          </DetailRow>
          {entry.notes && (
            <DetailRow label={t.admin.waitlist.detail.notes}>
              {entry.notes}
            </DetailRow>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="text-[12px] text-fg-3 w-20 shrink-0 pt-0.5">
        {label}
      </span>
      <div className="text-[14px] text-fg-2 min-w-0">{children}</div>
    </div>
  );
}

// --- Sub-components ---

function StatCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent?: boolean;
}) {
  return (
    <div className="rounded-surface border border-border/50 bg-card px-4 py-3">
      <div className="text-[12px] text-fg-3 mb-1">{label}</div>
      <div
        className={`text-xl font-semibold tabular-nums ${accent ? "text-fg-1" : "text-fg-2"}`}
      >
        {value}
      </div>
    </div>
  );
}

function CapacityCard({
  label,
  used,
  cap,
}: {
  label: string;
  used: number;
  cap: number;
}) {
  const pct = cap > 0 ? Math.min(100, Math.round((used / cap) * 100)) : 0;
  const displayCap = cap > 0 ? cap : "\u2014";

  return (
    <div className="rounded-surface border border-border/50 bg-card px-4 py-3">
      <div className="text-[12px] text-fg-3 mb-1">{label}</div>
      <div className="text-xl font-semibold tabular-nums text-fg-2">
        {used}
        <span className="text-[14px] font-normal text-fg-3">
          {" "}
          / {displayCap}
        </span>
      </div>
      {cap > 0 && (
        <div className="mt-2 h-1.5 rounded-full bg-surface-2 overflow-hidden">
          <div
            className="h-full rounded-full transition-all"
            style={{
              width: `${pct}%`,
              background:
                pct >= 90
                  ? "oklch(0.65 0.2 25)"
                  : "oklch(from var(--foreground) l c h / 0.25)",
            }}
          />
        </div>
      )}
    </div>
  );
}

function WaitlistRow({
  entry,
  selected,
  onSelect,
  onApprove,
  onReject,
  onRevokeInvite,
  onRestore,
  approving,
}: {
  entry: WaitlistEntry;
  selected: boolean;
  onSelect: (id: string) => void;
  onApprove: (id: string) => void;
  onReject: (id: string) => void;
  onRevokeInvite: (entryId: string) => void;
  onRestore: (entryId: string) => void;
  approving: boolean;
}) {
  const { t, locale } = useLocale();
  const statusLabel =
    t.admin.waitlist.status[
      entry.status as keyof typeof t.admin.waitlist.status
    ] ?? entry.status;

  const statusVariant =
    entry.status === "pending"
      ? "outline"
      : entry.status === "approved"
        ? "secondary"
        : entry.status === "registered"
          ? "default"
          : "destructive";

  const isPending = entry.status === "pending";

  return (
    <tr className="border-b border-border/20 hover:bg-surface-1 transition-colors">
      <td className="px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={() => onSelect(entry.id)}
          className="rounded cursor-pointer"
        />
      </td>
      <td className="px-4 py-3 text-fg-1 font-medium">{entry.email}</td>
      <td className="px-4 py-3 text-fg-2 hidden lg:table-cell">
        {t.admin.waitlist.useCase[
          entry.use_case as keyof typeof t.admin.waitlist.useCase
        ] ?? entry.use_case}
      </td>
      <td className="px-4 py-3 hidden xl:table-cell">
        <div className="flex flex-wrap gap-1">
          {entry.ai_tools?.map((tool) => (
            <span
              key={tool}
              className="inline-block px-1.5 py-0.5 rounded text-[11px] bg-surface-1 text-fg-3"
            >
              {tool}
            </span>
          ))}
        </div>
      </td>
      <td className="px-4 py-3 text-fg-3 hidden xl:table-cell">
        {entry.role
          ? (t.waitlistPage.roleOptions[
              entry.role as keyof typeof t.waitlistPage.roleOptions
            ] ?? entry.role)
          : ""}
      </td>
      <td className="px-4 py-3">
        <Badge variant={statusVariant}>{statusLabel}</Badge>
      </td>
      <td className="px-4 py-3 text-fg-3 hidden lg:table-cell">
        <InviteStatusCell entry={entry} />
      </td>
      <td className="px-4 py-3 text-fg-3 hidden md:table-cell">
        {formatDate(entry.created_at, locale)}
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-1">
          <EntryDetailDialog entry={entry} />
          {isPending && (
            <>
              <Button
                size="xs"
                variant="ghost"
                onClick={() => onApprove(entry.id)}
                disabled={approving}
                title={t.admin.waitlist.actions.approve}
              >
                <Check className="h-3.5 w-3.5" />
              </Button>
              <Button
                size="xs"
                variant="ghost"
                onClick={() => onReject(entry.id)}
                title={t.admin.waitlist.actions.reject}
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            </>
          )}
          {entry.status === "approved" && (
            <Button
              size="xs"
              variant="ghost"
              onClick={() => onRevokeInvite(entry.id)}
              title={t.admin.waitlist.actions.revokeInvite}
            >
              <Ban className="h-3.5 w-3.5" />
            </Button>
          )}
          {entry.status === "rejected" && (
            <Button
              size="xs"
              variant="ghost"
              onClick={() => onRestore(entry.id)}
              title={t.admin.waitlist.actions.undoReject}
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </td>
    </tr>
  );
}

function InviteStatusCell({ entry }: { entry: WaitlistEntry }) {
  const { t } = useLocale();
  const status = entry.invite_status;
  if (!status)
    return (
      <span className="text-fg-4">{t.admin.waitlist.inviteStatus.none}</span>
    );

  const label =
    t.admin.waitlist.inviteStatus[
      status as keyof typeof t.admin.waitlist.inviteStatus
    ] ?? status;

  const color =
    status === "active"
      ? "text-fg-2"
      : status === "used"
        ? "text-fg-3"
        : "text-fg-4";

  // When the invite was consumed, surface who consumed it as a
  // one-click link to /admin/users/{id}. The join fills name first,
  // falls back to email, falls back to just showing the id. Failing
  // to find any of those (old invite from before the join landed,
  // or a deleted user) collapses to the bare status label.
  if (status === "used" && entry.invite_used_by) {
    const display =
      entry.invite_used_by_name?.trim() ||
      entry.invite_used_by_email ||
      t.admin.waitlist.inviteStatus.usedUnknownUser;
    return (
      <span className={color}>
        {label}
        <span className="text-fg-4"> · </span>
        <Link
          href={`/admin/users/${entry.invite_used_by}`}
          className="text-fg-2 underline underline-offset-2 hover:text-fg-1"
          onClick={(e) => e.stopPropagation()}
        >
          {display}
        </Link>
      </span>
    );
  }

  return <span className={color}>{label}</span>;
}

const LOCALE_MAP: Record<string, string> = { en: "en-US", zh: "zh-CN" };

function formatDate(iso: string, locale: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(LOCALE_MAP[locale] ?? "en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}
