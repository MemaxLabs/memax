"use client";

// The admin panel's view of the V2 phase gates (adminV2MetricsClient):
// the gates, the north star, the cohorts by signup week and review
// health. Presentational only; the page fetches.

import type { ReactNode } from "react";
import { interpolate, useLocale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import type {
  AdminCohortMetrics,
  AdminPhaseGate,
  AdminReviewHealth,
  AdminV2Metrics,
} from "@/lib/admin-client";
import { EmptyState, OpsSection, StatCard } from "./ops-components";

type Copy = Translations["admin"]["infra"]["metrics"];

/** Seconds as 3m30s, 1h04m or 3d01h; null as the empty mark. */
export function formatSeconds(s: number | null, none: string): string {
  if (s === null) return none;
  const total = Math.round(s);
  if (total < 3600) {
    return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, "0")}s`;
  }
  const minutes = Math.floor(total / 60);
  if (total < 86400) {
    return `${Math.floor(minutes / 60)}h${String(minutes % 60).padStart(2, "0")}m`;
  }
  const hours = Math.floor(minutes / 60);
  return `${Math.floor(hours / 24)}d${String(hours % 24).padStart(2, "0")}h`;
}

/** A rate as a whole percentage. */
export function formatRate(r: number): string {
  return `${Math.round(100 * r)}%`;
}

function ratio(n: number, d: number): string {
  return `${n}/${d}`;
}

export function GateMetricsView({ data }: { data: AdminV2Metrics }) {
  const { t } = useLocale();
  const copy = t.admin.infra.metrics;
  const hours = data.first_session_hours;
  return (
    <div className="space-y-8">
      <OpsSection title={copy.gates.title} description={copy.gates.description}>
        <div className="grid gap-3 p-3 sm:grid-cols-2">
          {data.gates.map((g) => (
            <GateCard key={g.name} gate={g} copy={copy} />
          ))}
        </div>
      </OpsSection>

      <div className="grid gap-3 sm:grid-cols-2">
        <StatCard
          label={copy.northStar.title}
          value={interpolate(copy.northStar.value, {
            two: data.north_star.spaces_two_agents,
            read: data.north_star.spaces_read,
          })}
          hint={interpolate(copy.northStar.hint, {
            week: data.north_star.week_ending,
            coverage: formatRate(data.north_star.coverage),
            reading: data.north_star.connections_reading,
            seen: data.north_star.connections_seen,
          })}
        />
      </div>

      <OpsSection
        title={copy.cohorts.title}
        description={interpolate(copy.cohorts.description, { hours })}
      >
        {data.cohorts.length === 0 ? (
          <EmptyState>{copy.cohorts.empty}</EmptyState>
        ) : (
          <CohortTable
            cohorts={data.cohorts}
            totals={data.totals}
            copy={copy}
          />
        )}
      </OpsSection>

      <OpsSection
        title={copy.review.title}
        description={copy.review.description}
      >
        {data.review.length === 0 ? (
          <EmptyState>{copy.review.empty}</EmptyState>
        ) : (
          <ReviewTable
            rows={data.review}
            total={data.review_total}
            copy={copy}
          />
        )}
      </OpsSection>

      <section className="space-y-2">
        <h2 className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
          {copy.definitions.title}
        </h2>
        <ul className="max-w-3xl list-disc space-y-1 pl-5 text-[13px] leading-relaxed text-fg-2">
          {(
            [
              "signup",
              "session",
              "activated",
              "firstFile",
              "week4",
              "teammate",
              "review",
            ] as const
          ).map((k) => (
            <li key={k}>{interpolate(copy.definitions[k], { hours })}</li>
          ))}
        </ul>
      </section>
    </div>
  );
}

function GateCard({ gate, copy }: { gate: AdminPhaseGate; copy: Copy }) {
  const file = gate.name === "first_file";
  const show = (v: number) =>
    file ? formatSeconds(v, copy.none) : formatRate(v);
  const value = gate.value === null ? copy.none : show(gate.value);
  const hint = interpolate(file ? copy.gates.fileHint : copy.gates.ratioHint, {
    numerator: gate.numerator,
    denominator: gate.denominator,
    bar: show(gate.bar),
  });
  // The ops panel's job-state tones: met is the completed green, below
  // the bar an amber, waiting the neutral chip.
  const pill = {
    pass: "border-emerald-500/30 bg-emerald-500/10 text-fg-1",
    fail: "border-amber-500/40 bg-amber-500/10 text-fg-1",
    pending: "border-border/60 bg-surface-2 text-fg-3",
  }[gate.status];
  return (
    <div
      className="flex flex-col gap-1 rounded-2xl border border-border/60 bg-background px-4 py-3"
      data-gate={gate.name}
      data-status={gate.status}
    >
      <div className="text-[12px] font-medium text-fg-2">
        {copy.gates[gate.name]}
      </div>
      <div className="flex items-baseline gap-2">
        <span className="text-[26px] font-semibold tabular-nums leading-none text-fg-1">
          {value}
        </span>
        <span
          className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium ${pill}`}
        >
          {copy.gates.status[gate.status]}
        </span>
      </div>
      <div className="text-[12px] tabular-nums text-fg-3">{hint}</div>
      {gate.from_v1 !== null ? (
        <div className="text-[12px] tabular-nums text-fg-3">
          {interpolate(copy.gates.fromV1, { value: show(gate.from_v1) })}
        </div>
      ) : null}
    </div>
  );
}

function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-[13px]">
        <thead>
          <tr className="text-left text-[11px] uppercase tracking-wider text-fg-3">
            {head.map((h, i) => (
              <th key={i} className="whitespace-nowrap px-2.5 py-2 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

function Cells({ cells, strong }: { cells: ReactNode[]; strong?: boolean }) {
  return (
    <tr
      className={`border-t border-border/30 tabular-nums ${strong ? "font-medium text-fg-1" : "text-fg-2"}`}
    >
      {cells.map((c, i) => (
        <td key={i} className="whitespace-nowrap px-2.5 py-2">
          {c}
        </td>
      ))}
    </tr>
  );
}

function CohortTable({
  cohorts,
  totals,
  copy,
}: {
  cohorts: AdminCohortMetrics[];
  totals: AdminCohortMetrics[];
  copy: Copy;
}) {
  const c = copy.cohorts.columns;
  const row = (week: string, m: AdminCohortMetrics, strong?: boolean) => (
    <Cells
      key={`${week}-${m.cohort}`}
      strong={strong}
      cells={[
        week,
        copy.cohorts.kind[m.cohort],
        m.people,
        m.sessions_closed,
        m.sessions_closed > 0
          ? `${m.activated} (${formatRate(m.activated / m.sessions_closed)})`
          : m.activated,
        m.two_connections,
        m.compiled,
        `${formatSeconds(m.first_file_p50_seconds, copy.none)} / ${formatSeconds(m.first_file_p90_seconds, copy.none)}`,
        ratio(m.first_files_under_5m, m.first_files),
        ratio(m.retained, m.retention_eligible),
        ratio(m.team_pulled, m.team_eligible),
        m.agents_read,
        formatSeconds(m.signup_to_file_p50_seconds, copy.none),
      ]}
    />
  );
  return (
    <Table
      head={[
        c.week,
        c.cohort,
        c.people,
        c.closed,
        c.activated,
        c.twoAgents,
        c.compiled,
        c.firstFile,
        c.under5m,
        c.week4,
        c.teammate,
        c.read,
        c.signupToFile,
      ]}
    >
      {cohorts.map((m) => row(m.week?.slice(0, 10) ?? "", m))}
      {totals.map((m) => row(copy.cohorts.all, m, true))}
    </Table>
  );
}

function ReviewTable({
  rows,
  total,
  copy,
}: {
  rows: AdminReviewHealth[];
  total: AdminReviewHealth;
  copy: Copy;
}) {
  const c = copy.review.columns;
  const row = (week: string, r: AdminReviewHealth, strong?: boolean) => {
    const decided = r.kept + r.rejected;
    return (
      <Cells
        key={week}
        strong={strong}
        cells={[
          week,
          r.proposals,
          r.kept,
          r.rejected,
          r.folded,
          r.forgotten,
          r.open,
          decided > 0 ? formatRate(r.rejected / decided) : copy.none,
          formatSeconds(r.decision_p50_seconds, copy.none),
          formatSeconds(r.decision_p90_seconds, copy.none),
        ]}
      />
    );
  };
  return (
    <Table
      head={[
        c.week,
        c.proposals,
        c.kept,
        c.rejected,
        c.folded,
        c.forgotten,
        c.open,
        c.rejectRate,
        c.decision,
        c.decisionP90,
      ]}
    >
      {rows.map((r) => row(r.week?.slice(0, 10) ?? "", r))}
      {row(copy.cohorts.all, total, true)}
    </Table>
  );
}
