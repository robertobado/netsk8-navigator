import type { ReactNode } from 'react'
import { AlertTriangle, CircleCheck, CircleOff } from 'lucide-react'
import type { PV, PVC } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { StatusBadge } from './StatusBadge'
import { HoverBubble } from './HoverBubble'

// Cell components for the resource tables. They live apart from the resource
// catalog (lib/resources.tsx) so that file exports only data — keeping React
// Fast Refresh's component-only-export rule satisfied on both sides.

// Deployment rollout status with a tone/icon per state (mirrors the pods column):
// Available → green check; Progressing → blue rolling dots; Scaled to 0 → muted.
export function DeploymentStatus({ status }: Readonly<{ status: string }>) {
  const pill = (className: string, children: ReactNode) => (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', className)}>{children}</span>
  )
  if (status === 'Available')
    return pill(
      'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25',
      <>
        <CircleCheck className="size-3" /> Available
      </>,
    )
  if (status === 'Progressing')
    return pill(
      'bg-[#38bdf8]/12 text-[#38bdf8] ring-[#38bdf8]/30',
      <>
        Progressing
        <span className="inline-flex items-end gap-0.5">
          {[0, 1, 2].map((i) => (
            <span key={i} className="size-1 animate-bounce rounded-full bg-[#38bdf8]" style={{ animationDelay: `${i * 0.15}s` }} />
          ))}
        </span>
      </>,
    )
  if (status.startsWith('Scaled'))
    return pill(
      'bg-muted text-muted-foreground ring-border',
      <>
        <CircleOff className="size-3" /> {status}
      </>,
    )
  return <StatusBadge status={status} />
}

const JOB_TONE: Record<string, string> = { Complete: 'var(--ok)', Failed: 'var(--err)', Running: 'var(--brand)', Suspended: 'var(--muted-foreground)' }
export function JobStatus({ status }: Readonly<{ status: string }>) {
  const known = status in JOB_TONE
  const color = known ? JOB_TONE[status] : 'var(--err)' // an unknown status = a pod-level error reason
  return (
    <span className="inline-flex items-center gap-1 text-sm font-medium" style={{ color }}>
      {!known && <AlertTriangle className="size-3" />}
      {status}
    </span>
  )
}

// PVC "Status" cell: the phase badge with a hover balloon naming the bound PV
// (capacity/class/access already show as columns — for a Bound PVC they are, by
// construction, identical to the PV's, so we only add the PV name here).
export function PVCStatusCell({ pvc }: Readonly<{ pvc: PVC }>) {
  const content =
    pvc.status === 'Bound' ? (
      <div className="min-w-40 space-y-1">
        <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">Volume (PV)</div>
        <div className="font-mono text-[11px] text-[color:var(--brand)]">{pvc.volume || '—'}</div>
      </div>
    ) : (
      <div className="max-w-56 text-muted-foreground">Ainda não vinculado a um PersistentVolume.</div>
    )
  return (
    <HoverBubble content={content}>
      <StatusBadge status={pvc.status} />
    </HoverBubble>
  )
}

// "Montado" cell: a Sim/Não badge; when mounted, a hover balloon lists the pods
// (all in the PVC's own namespace).
export function MountedCell({ pvc }: Readonly<{ pvc: PVC }>) {
  const pods = pvc.mountedBy ?? []
  const mounted = pods.length > 0
  let label = 'Não'
  if (mounted) label = pods.length > 1 ? `Sim (${pods.length})` : 'Sim'
  const badge = (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        mounted ? 'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25' : 'bg-muted text-muted-foreground ring-border',
      )}
    >
      {mounted ? <CircleCheck className="size-3" /> : <CircleOff className="size-3" />}
      {label}
    </span>
  )
  if (!mounted) return badge
  const content = (
    <div className="min-w-48 space-y-2">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">Montado por</div>
      {pods.map((p) => (
        <div key={p.pod} className="flex flex-col leading-tight">
          <span className="font-medium">{p.pod}</span>
          <span className="font-mono text-[11px] text-muted-foreground">{pvc.namespace}</span>
          <div className="mt-1 space-y-0.5">
            {(p.mounts ?? []).map((m) => (
              <div key={`${m.container}:${m.path}`} className="flex items-baseline gap-1.5 font-mono text-[11px]">
                <span className="shrink-0 rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">{m.container}</span>
                <span className="min-w-0 break-all text-foreground/90">{m.path}</span>
              </div>
            ))}
            {(p.mounts ?? []).length === 0 && <span className="text-[11px] text-muted-foreground/70">volume referenciado, sem mountPath</span>}
          </div>
        </div>
      ))}
    </div>
  )
  return <HoverBubble content={content}>{badge}</HoverBubble>
}

// Compact PV row shown inside an expanded StorageClass.
export function PVChildRow({ pv }: Readonly<{ pv: PV }>) {
  return (
    <>
      <span className="min-w-0 flex-1 truncate font-medium text-[color:var(--brand)]">{pv.name}</span>
      <StatusBadge status={pv.status} />
      <span className="w-16 shrink-0 text-right font-mono text-muted-foreground tabular-nums">{pv.capacity || '—'}</span>
      <span className="w-48 shrink-0 truncate font-mono text-muted-foreground" title={pv.claim}>
        {pv.claim || '—'}
      </span>
      <span className="w-10 shrink-0 text-right font-mono text-muted-foreground tabular-nums">{age(pv.age)}</span>
    </>
  )
}
