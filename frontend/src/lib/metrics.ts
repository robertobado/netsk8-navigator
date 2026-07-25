import { setAppPrefs, useAppPrefs } from '@/lib/preferences'

// Metrics refresh setting (ms; 0 = off → all metrics hidden), stored in app
// preferences. Kept in lib/ (not the control component) so it can be shared
// without tripping React Fast Refresh's component-only-export rule.

export const REFRESH_OPTIONS: ReadonlyArray<{ ms: number; label: string }> = [
  { ms: 0, label: 'Off' },
  { ms: 5_000, label: '5s' },
  { ms: 15_000, label: '15s' },
  { ms: 30_000, label: '30s' },
  { ms: 60_000, label: '1m' },
]

export function setMetricsRefresh(ms: number) {
  setAppPrefs({ metricsRefreshMs: ms })
}

/** Current metrics refresh: `ms` raw setting, `interval` = ms or null when off. */
export function useMetricsRefresh(): { ms: number; interval: number | null } {
  const ms = useAppPrefs().metricsRefreshMs
  return { ms, interval: ms > 0 ? ms : null }
}
