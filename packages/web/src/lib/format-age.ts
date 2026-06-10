/**
 * Formats a date string as a relative time (e.g., "2d ago").
 * Requires i18n interpolate function and translation object.
 */
export function formatAge(
  dateStr: string,
  t: {
    time: {
      justNow: string;
      mAgo: string;
      hAgo: string;
      dAgo: string;
      wAgo: string;
      moAgo: string;
    };
  },
  interpolate: (
    template: string,
    vars: Record<string, string | number>,
  ) => string,
): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return t.time.justNow;
  if (mins < 60) return interpolate(t.time.mAgo, { n: mins });
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return interpolate(t.time.hAgo, { n: hrs });
  const days = Math.floor(hrs / 24);
  if (days < 7) return interpolate(t.time.dAgo, { n: days });
  if (days < 30) return interpolate(t.time.wAgo, { n: Math.floor(days / 7) });
  return interpolate(t.time.moAgo, { n: Math.floor(days / 30) });
}
