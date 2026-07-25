import { cn } from '@/lib/utils'
import type { Gauge, PodUsageEntry } from '@/lib/api'
import { fmtBytes, fmtCores, type UsageBasis } from '@/lib/usage'

// Shared inline usage gauges, used by the pods and deployments tables. A tiny
// 270° arc showing usage vs. its ceiling, with % + absolute value.

const MG_SPAN = 270
const MG_START = -135
const MG_CX = 15
const MG_CY = 14
const MG_R = 10
function mgPoint(deg: number): [number, number] {
  const a = (deg * Math.PI) / 180
  return [MG_CX + MG_R * Math.sin(a), MG_CY - MG_R * Math.cos(a)]
}
function mgArc(f0: number, f1: number): string {
  const a0 = MG_START + f0 * MG_SPAN
  const a1 = MG_START + f1 * MG_SPAN
  const [sx, sy] = mgPoint(a0)
  const [ex, ey] = mgPoint(a1)
  return `M ${sx} ${sy} A ${MG_R} ${MG_R} 0 ${a1 - a0 > 180 ? 1 : 0} 1 ${ex} ${ey}`
}

// A tiny arc gauge for a single metric: % on top, absolute below, colored by zone.
export function MiniGauge({ g, kind }: Readonly<{ g?: Gauge; kind: 'cores' | 'bytes' }>) {
  const fmt = kind === 'cores' ? fmtCores : fmtBytes
  const used = g?.used ?? 0
  const total = g?.total ?? 0 // effective ceiling (limit→request)
  const frac = total > 0 ? Math.min(1, used / total) : 0
  let color = 'var(--muted-foreground)'
  if (total > 0) color = frac >= 0.9 ? 'var(--err)' : frac >= 0.8 ? 'var(--warn)' : 'var(--ok)'
  const label = kind === 'cores' ? 'C' : 'M'
  let ceilTxt = 'sem limite'
  if (total > 0) ceilTxt = `${(g?.limit ?? 0) > 0 ? fmt(g!.limit!) : fmt(g!.request ?? 0)} (${Math.round(frac * 100)}%)`
  const title = `${kind === 'cores' ? 'CPU' : 'Memória'}: ${fmt(used)} / ${ceilTxt}`
  return (
    <span className="inline-flex items-center gap-1.5" title={title}>
      <span className="relative">
        <svg width="30" height="26" viewBox="0 0 30 28" aria-hidden>
          <path d={mgArc(0, 1)} fill="none" stroke="var(--border)" strokeWidth="3" strokeLinecap="round" />
          {total > 0 && frac > 0 && <path d={mgArc(0, frac)} fill="none" stroke={color} strokeWidth="3" strokeLinecap="round" />}
        </svg>
        <span className="absolute inset-x-0 top-[6px] text-center text-[9px] font-semibold leading-none" style={{ color }}>
          {label}
        </span>
      </span>
      <span className="flex flex-col leading-tight">
        <span className="text-[11px] font-semibold tabular-nums" style={{ color: total > 0 ? color : 'var(--muted-foreground)' }}>
          {total > 0 ? `${Math.round(frac * 100)}%` : '—'}
        </span>
        <span className="text-[10px] text-muted-foreground tabular-nums">{fmt(used)}</span>
      </span>
    </span>
  )
}

// Header toggle choosing whether a usage column sorts by % or absolute value.
export function UsageBasisToggle({ basis, onChange }: Readonly<{ basis: UsageBasis; onChange: (b: UsageBasis) => void }>) {
  return (
    <span className="inline-flex overflow-hidden rounded-md border text-[9px] font-medium">
      {(['pct', 'abs'] as const).map((b) => (
        <button
          key={b}
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onChange(b)
          }}
          title={b === 'pct' ? 'Ordenar por % de utilização' : 'Ordenar por valor absoluto'}
          className={cn('px-1 py-0.5 transition-colors', basis === b ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground')}
        >
          {b === 'pct' ? '%' : 'val'}
        </button>
      ))}
    </span>
  )
}

export type { PodUsageEntry }
