// Maps to: docs/plans/11-design-unification.md checklist
// ui/ components: coverage status reference
"use client";

import { Section, DemoCard } from "../_shared";

const MATRIX = [
  {
    location: "Bar recall",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
  {
    location: "Bar remember",
    loading: "done",
    empty: "na",
    error: "done",
    processing: "na",
  },
  {
    location: "Bar AI synthesis",
    loading: "done",
    empty: "na",
    error: "done",
    processing: "na",
  },
  {
    location: "Memory grid",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "done",
  },
  {
    location: "Note detail",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "done",
  },
  {
    location: "Settings",
    loading: "partial",
    empty: "missing",
    error: "done",
    processing: "na",
  },
  {
    location: "Brain view",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
  {
    location: "Topics page",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
  {
    location: "Topic detail",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
  {
    location: "Topic tree panel",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
  {
    location: "Recent section",
    loading: "done",
    empty: "done",
    error: "done",
    processing: "na",
  },
];

function StatusIcon({ status }: { status: string }) {
  if (status === "done") return <span title="Implemented">&#x2705;</span>;
  if (status === "partial")
    return <span title="Partial">&#x26A0;&#xFE0F;</span>;
  if (status === "missing") return <span title="Missing">&#x274C;</span>;
  return (
    <span className="text-fg-4" title="N/A">
      &mdash;
    </span>
  );
}

export function CoverageGapsSection() {
  return (
    <Section
      title="22. Coverage Gaps"
      description="State coverage matrix — what's implemented, partial, or missing across every surface."
    >
      <DemoCard label="Status matrix">
        <div className="overflow-x-auto">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="text-fg-3 text-left">
                <th className="pb-2 pr-4 font-medium">Location</th>
                <th className="pb-2 px-3 font-medium text-center">Loading</th>
                <th className="pb-2 px-3 font-medium text-center">Empty</th>
                <th className="pb-2 px-3 font-medium text-center">Error</th>
                <th className="pb-2 px-3 font-medium text-center">
                  Processing
                </th>
              </tr>
            </thead>
            <tbody>
              {MATRIX.map((row) => (
                <tr key={row.location} className="border-t border-border/20">
                  <td className="py-2 pr-4 text-fg-2">{row.location}</td>
                  <td className="py-2 px-3 text-center">
                    <StatusIcon status={row.loading} />
                  </td>
                  <td className="py-2 px-3 text-center">
                    <StatusIcon status={row.empty} />
                  </td>
                  <td className="py-2 px-3 text-center">
                    <StatusIcon status={row.error} />
                  </td>
                  <td className="py-2 px-3 text-center">
                    <StatusIcon status={row.processing} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="mt-4 pt-3 border-t border-border/20 space-y-1">
          <p className="text-[12px] text-fg-3">
            &#x2705; implemented &middot; &#x26A0;&#xFE0F; partial &middot;
            &#x274C; missing &middot; &mdash; N/A
          </p>
          <p className="text-[12px] text-fg-4">
            <strong className="text-fg-3">Current focus:</strong> loading and
            async-state consistency, not baseline error coverage
          </p>
        </div>
      </DemoCard>
    </Section>
  );
}
