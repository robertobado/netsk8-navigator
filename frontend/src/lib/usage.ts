import type { Gauge } from '@/lib/api'

// Usage formatting + sort helpers shared by the pods/deployments tables and the
// metrics section. Kept in lib/ (apart from the gauge components) so React Fast
// Refresh stays happy.

export type UsageBasis = 'pct' | 'abs'

export function fmtCores(v: number): string {
  return v >= 1 ? v.toFixed(2) : v.toFixed(3)
}

export function fmtBytes(v: number): string {
  const u = ['B', 'Ki', 'Mi', 'Gi', 'Ti']
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)}${u[i]}`
}

// Value a usage column sorts by: absolute used, or % of ceiling. Missing → -1 (sinks).
export function usageSortValue(g: Gauge | undefined, basis: UsageBasis): number {
  if (!g) return -1
  if (basis === 'abs') return g.used
  return g.total > 0 ? g.used / g.total : -1
}

// Persisted per-metric basis state (localStorage key `netsk8.usage.<id>`).
export function readBasis(id: string): UsageBasis {
  return (localStorage.getItem(`netsk8.usage.${id}`) as UsageBasis) || 'pct'
}
export function writeBasis(id: string, b: UsageBasis) {
  localStorage.setItem(`netsk8.usage.${id}`, b)
}
