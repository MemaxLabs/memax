"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";

import {
  adminV2MetricsClient,
  type AdminV2Metrics,
  type AdminV2MetricsRange,
} from "@/lib/admin-client";

/**
 * useAdminV2Metrics reads the phase gates' metrics for a range of signup
 * weeks. They change once a person's window closes, so a minute's
 * staleness is plenty; switching ranges keeps the last table up while the
 * next one loads.
 */
export function useAdminV2Metrics(range: AdminV2MetricsRange) {
  return useQuery<AdminV2Metrics>({
    queryKey: ["admin", "v2-metrics", range.from, range.to],
    queryFn: () => adminV2MetricsClient.get(range),
    staleTime: 60 * 1000,
    placeholderData: keepPreviousData,
  });
}
