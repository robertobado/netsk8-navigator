import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery } from '@tanstack/react-query'
import { createColumnHelper, type ColumnDef } from '@tanstack/react-table'
import { AlertTriangle, CircleCheck, Clock, ExternalLink, Loader2, Pin, Radio, WifiOff } from 'lucide-react'
import { api, kindToSlug, type Pod, type PodUsageEntry } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { StatusBadge } from './StatusBadge'
import { DataTable } from './DataTable'
import { HoverBubble } from './HoverBubble'
import { useMetricsRefresh } from '@/lib/metrics'
import { MiniGauge, UsageBasisToggle } from './UsageGauge'
import { usageSortValue, readBasis, writeBasis, type UsageBasis } from '@/lib/usage'
import type { DrawerTarget } from './ResourceDrawer'

// --- Status reason triage (see kubelet container states) --------------------
// Transient: normal startup / scheduling, self-resolves.
const TRANSIENT_REASONS = new Set(['ContainerCreating', 'PodInitializing', 'Unschedulable'])
// Terminal-but-fine: finished successfully.
const OK_REASONS = new Set(['Completed'])
// Everything else with a reason is treated as an ERROR (image/config/runtime/crash/
// OOM/sandbox/eviction…), so unknown reasons fail safe to "needs attention".

type StatusGroup = 'running' | 'transient' | 'error' | 'ok' | 'terminating' | 'default'
function classify(status: string, reason: string): StatusGroup {
  if (reason) {
    if (TRANSIENT_REASONS.has(reason)) return 'transient'
    if (OK_REASONS.has(reason)) return 'ok'
    return 'error'
  }
  switch (status) {
    case 'Running':
      return 'running'
    case 'Succeeded':
      return 'ok'
    case 'Failed':
      return 'error'
    case 'Pending':
      return 'transient'
    case 'Terminating':
      return 'terminating'
    default:
      return 'default'
  }
}

const GROUP_STYLE: Record<string, string> = {
  error: 'bg-[color:var(--err)]/12 text-[color:var(--err)] ring-[color:var(--err)]/25',
  transient: 'bg-[#38bdf8]/12 text-[#38bdf8] ring-[#38bdf8]/30',
  ok: 'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25',
  terminating: 'bg-muted text-muted-foreground ring-border',
}


// Live-ticking relative age from an ISO timestamp (updates every second).
function LiveAge({ since }: Readonly<{ since: string }>) {
  const [, tick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => tick((t) => t + 1), 1000)
    return () => clearInterval(id)
  }, [])
  return <>{age(since)}</>
}

// Warning event reasons that specifically point at a stuck/failing termination.
const TERM_PROBLEM_REASONS = /(kill|prestop|delete|terminat|graceful|unmount|detach|evict)/i

// Terminating cell. A warning replaces the ellipsis when either:
//  (a) the pod has a Warning event about the termination itself, or
//  (b) it has finalizers and has been Terminating > 5min.
// The bubble shows whichever detail drove the warning. Otherwise: a grey ellipsis.
function TerminatingStatus({ ctx, namespace, name, deletedAt, finalizers }: Readonly<{ ctx: string; namespace: string; name: string; deletedAt?: string; finalizers: string[] }>) {
  const [, tick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => tick((t) => t + 1), 1000)
    return () => clearInterval(id)
  }, [])
  const eventsQ = useQuery({
    queryKey: ['events', ctx, namespace, name, 'Pod'],
    queryFn: () => api.events(ctx, namespace, name, 'Pod'),
    refetchInterval: 15_000,
  })

  const sinceMs = deletedAt ? new Date(deletedAt).getTime() : 0
  const problemEvents = (eventsQ.data ?? []).filter(
    (e) => e.type === 'Warning' && (TERM_PROBLEM_REASONS.test(e.reason) || (sinceMs > 0 && new Date(e.last).getTime() >= sinceMs - 5_000)),
  )
  const elapsedMs = deletedAt ? Date.now() - sinceMs : 0
  const stuckFinalizers = problemEvents.length === 0 && finalizers.length > 0 && elapsedMs > 5 * 60_000
  const warn = problemEvents.length > 0 || stuckFinalizers

  const badge = (
    <span className="inline-flex cursor-default items-center gap-2">
      <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', GROUP_STYLE.terminating)}>
        Terminating
        {warn ? (
          <AlertTriangle className="size-3 text-[color:var(--warn)]" />
        ) : (
          <span className="inline-flex items-end gap-0.5">
            {[0, 1, 2].map((i) => (
              <span key={i} className="size-1 animate-bounce rounded-full bg-current opacity-70" style={{ animationDelay: `${i * 0.15}s` }} />
            ))}
          </span>
        )}
      </span>
      {deletedAt && (
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="size-3" />
          <LiveAge since={deletedAt} />
        </span>
      )}
    </span>
  )

  if (!warn) return badge

  const content =
    problemEvents.length > 0 ? (
      <>
        <div className="mb-1 font-medium text-[color:var(--warn)]">Problemas no término</div>
        <ul className="space-y-1">
          {problemEvents.slice(0, 4).map((e, i) => (
            <li key={`${e.reason}-${i}`}>
              <span className="font-medium">{e.reason}</span>
              {e.count > 1 && <span className="text-muted-foreground"> ×{e.count}</span>}
              <p className="break-words text-[11px] text-muted-foreground">{e.message}</p>
            </li>
          ))}
        </ul>
      </>
    ) : (
      <>
        <div className="mb-1 font-medium text-[color:var(--warn)]">Finalizers pendentes ({finalizers.length})</div>
        <ul className="space-y-0.5">
          {finalizers.map((f) => (
            <li key={f} className="break-all font-mono text-[11px]">
              {f}
            </li>
          ))}
        </ul>
      </>
    )

  return <HoverBubble content={content}>{badge}</HoverBubble>
}

// Status cell: badge text = kubectl-style effective status (the reason when a
// container isn't healthy, even in phase Running), colored + iconed by group.
function PodStatus({ status, reason, deletedAt, finalizers, ctx, namespace, name, createdAt }: Readonly<{ status: string; reason: string; deletedAt?: string; finalizers?: string[]; ctx: string; namespace: string; name: string; createdAt?: string }>) {
  const group = classify(status, reason)
  const text = reason || status

  if (group === 'running') {
    return (
      <span className="inline-flex items-center gap-2">
        <StatusBadge status="Running" />
        <span className="relative flex size-2.5" title="Ativo">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[color:var(--ok)] opacity-60" />
          <span className="relative inline-flex size-2.5 rounded-full bg-[color:var(--ok)]" />
        </span>
      </span>
    )
  }
  // terminating → dedicated component (events/finalizers drive warning vs ellipsis)
  if (group === 'terminating') {
    return <TerminatingStatus ctx={ctx} namespace={namespace} name={name} deletedAt={deletedAt} finalizers={finalizers ?? []} />
  }

  const pill = (children: React.ReactNode) => (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', GROUP_STYLE[group])}>
      {children}
    </span>
  )
  let content: React.ReactNode
  if (group === 'error') content = pill(<><AlertTriangle className="size-3" /> {text}</>)
  else if (group === 'ok') content = pill(<><CircleCheck className="size-3" /> {text}</>)
  else if (group === 'transient')
    content = pill(
      <>
        {text}
        <span className="inline-flex items-end gap-0.5">
          {[0, 1, 2].map((i) => (
            <span key={i} className="size-1 animate-bounce rounded-full bg-[#38bdf8]" style={{ animationDelay: `${i * 0.15}s` }} />
          ))}
        </span>
      </>,
    )
  else content = <StatusBadge status={text} />

  // Pending: show the time-in-pending inline; reveal the error detail on hover.
  if (status === 'Pending') {
    return (
      <PendingStatus ctx={ctx} namespace={namespace} name={name} createdAt={createdAt}>
        {content}
      </PendingStatus>
    )
  }
  return content
}

// Pending status: the badge + a live time-in-pending counter inline. The error
// detail (and a pin) are revealed only while hovering the Status badge area, so
// non-hovered rows all keep the same height.
function PendingStatus({ ctx, namespace, name, createdAt, children }: Readonly<{ ctx: string; namespace: string; name: string; createdAt?: string; children: React.ReactNode }>) {
  const ref = useRef<HTMLSpanElement>(null)
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null)
  const [hovering, setHovering] = useState(false)
  const [pinned, setPinned] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const open = hovering || pinned

  const q = useQuery({
    queryKey: ['pending', ctx, namespace, name],
    queryFn: () => api.podPending(ctx, namespace, name),
    enabled: open,
    refetchInterval: pinned ? 15_000 : false,
  })
  const d = q.data

  const show = () => {
    clearTimeout(timer.current)
    const r = ref.current?.getBoundingClientRect()
    if (r) setPos({ x: r.left, y: r.bottom + 6 })
    setHovering(true)
  }
  const scheduleClose = () => {
    timer.current = setTimeout(() => setHovering(false), 120)
  }
  const showBubble = open && pos && (q.isLoading || !!d?.message)

  return (
    <span ref={ref} className="inline-flex cursor-default items-center gap-2" onMouseEnter={show} onMouseLeave={scheduleClose}>
      {children}
      {createdAt && (
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="size-3" />
          <LiveAge since={createdAt} />
        </span>
      )}
      {showBubble &&
        createPortal(
          <div
            style={{ position: 'fixed', left: pos.x, top: pos.y, zIndex: 60 }}
            onMouseEnter={show}
            onMouseLeave={scheduleClose}
            className="w-max max-w-md rounded-lg border bg-popover/95 p-2.5 text-xs shadow-2xl shadow-black/40 backdrop-blur-xl"
          >
            <div className="mb-1 flex items-center justify-between gap-6">
              <span className="font-medium text-muted-foreground">Detalhe</span>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  setPinned((v) => !v)
                }}
                title={pinned ? 'Desafixar' : 'Fixar'}
                className={cn('inline-flex items-center gap-1 rounded px-1 text-[10px] font-medium transition-colors', pinned ? 'text-[color:var(--brand)]' : 'text-muted-foreground hover:text-foreground')}
              >
                <Pin className={cn('size-3 transition-transform', pinned ? 'rotate-0 fill-current' : 'rotate-45')} />
                {pinned ? 'fixado' : 'fixar'}
              </button>
            </div>
            {q.isLoading ? (
              <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                <Loader2 className="size-3 animate-spin" /> carregando…
              </span>
            ) : (
              <p className="break-words text-muted-foreground">{d?.message}</p>
            )}
          </div>,
          document.body,
        )}
    </span>
  )
}

type ConnState = 'connecting' | 'live' | 'error'

function LiveIndicator({ state }: Readonly<{ state: ConnState }>) {
  if (state === 'live')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-[color:var(--ok)]/12 px-2 py-0.5 text-xs font-medium text-[color:var(--ok)] ring-1 ring-inset ring-[color:var(--ok)]/25">
        <Radio className="size-3 animate-pulse" /> ao vivo
      </span>
    )
  if (state === 'error')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-[color:var(--err)]/12 px-2 py-0.5 text-xs font-medium text-[color:var(--err)] ring-1 ring-inset ring-[color:var(--err)]/25">
        <WifiOff className="size-3" /> reconectando
      </span>
    )
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground ring-1 ring-inset ring-border">
      <Radio className="size-3 animate-pulse" /> conectando
    </span>
  )
}

// A cell link that opens a detail drawer without triggering the row click.
function ResourceLink({ label, onOpen }: Readonly<{ label: string; onOpen: () => void }>) {
  return (
    <button
      onClick={(e) => {
        e.stopPropagation()
        onOpen()
      }}
      className="group inline-flex max-w-full items-center gap-1 truncate text-[color:var(--brand)] transition-colors hover:text-foreground"
    >
      <span className="truncate underline decoration-dotted underline-offset-2">{label}</span>
      <ExternalLink className="size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-70" />
    </button>
  )
}

// Row = pod + its usage snapshot, so sorting sees fresh metrics. Attaching usage
// to the row (rather than reading an external map in the accessor) makes `data`
// change on every metrics refetch, which re-runs TanStack's sort.
type PodRow = Pod & { usage?: PodUsageEntry }
const col = createColumnHelper<PodRow>()

export function PodsTable({
  ctx,
  ns,
  pods,
  connState,
  onSelect,
  onOpenResource,
}: Readonly<{
  ctx: string
  ns?: string
  pods: Pod[]
  connState: ConnState
  onSelect?: (p: Pod) => void
  onOpenResource: (t: DrawerTarget) => void
}>) {
  // Batch per-pod usage (one call for all pods) → inline mini gauges. The column
  // only appears when the cluster has metrics-server and metrics aren't turned off.
  const { interval } = useMetricsRefresh()
  const usageQ = useQuery({
    queryKey: ['podusage', ctx, ns],
    queryFn: () => api.podsUsage(ctx, ns || undefined),
    enabled: interval != null,
    refetchInterval: interval ?? false,
  })
  const usage = interval != null && usageQ.data?.available ? usageQ.data.items : undefined
  const hasUsage = !!usage

  // Per-metric sort basis (% vs absolute), persisted. Combined with the column's
  // asc/desc gives the four sort options per metric.
  const [cpuBasis, setCpuBasis] = useState<UsageBasis>(() => readBasis('cpu'))
  const [memBasis, setMemBasis] = useState<UsageBasis>(() => readBasis('mem'))
  useEffect(() => writeBasis('cpu', cpuBasis), [cpuBasis])
  useEffect(() => writeBasis('mem', memBasis), [memBasis])

  // Attach each pod's usage to its row. New array on every metrics refetch → re-sort.
  const rows = useMemo<PodRow[]>(
    () => (usage ? pods.map((p) => ({ ...p, usage: usage[`${p.namespace}/${p.name}`] })) : pods),
    [pods, usage],
  )

  const columns = useMemo(
    () => {
      const cols = [
        col.accessor('name', { header: 'Nome', cell: (c) => <span className="font-medium">{c.getValue()}</span> }),
        col.accessor('namespace', { header: 'Namespace', cell: (c) => <span className="text-muted-foreground">{c.getValue()}</span> }),
        col.accessor('status', { header: 'Status', cell: (c) => <PodStatus status={c.getValue()} reason={c.row.original.reason} deletedAt={c.row.original.deletedAt} finalizers={c.row.original.finalizers} createdAt={c.row.original.age} ctx={ctx} namespace={c.row.original.namespace} name={c.row.original.name} /> }),
        col.accessor((r) => `${r.ready}/${r.total}`, {
          id: 'ready',
          header: 'Ready',
          cell: (c) => {
            const r = c.row.original
            // Fully-ready, or a pod that finished successfully (Completed/Succeeded),
            // reads as green — a finished Job pod shows 0/1 but isn't unhealthy.
            const healthy = (r.ready === r.total && r.total > 0) || classify(r.status, r.reason) === 'ok'
            return (
              <span className={cn('font-mono text-sm tabular-nums', healthy ? 'text-[color:var(--ok)]' : 'text-[color:var(--warn)]')}>
                {c.getValue()}
              </span>
            )
          },
        }),
        col.accessor('restarts', {
          header: 'Restarts',
          cell: (c) => {
            const n = c.getValue()
            return <span className={cn('font-mono text-sm tabular-nums', n > 0 ? 'text-[color:var(--warn)]' : 'text-muted-foreground')}>{n}</span>
          },
        }),
        col.accessor((r) => (r.ownerKind ? `${r.ownerKind}/${r.ownerName}` : ''), {
          id: 'controlledBy',
          header: 'Controlled By',
          cell: (c) => {
            const p = c.row.original
            const slug = p.ownerKind ? kindToSlug(p.ownerKind) : null
            if (!slug) return <span className="text-muted-foreground">—</span>
            return (
              <span className="flex items-center gap-1.5">
                <span className="text-xs text-muted-foreground">{p.ownerKind}</span>
                <ResourceLink
                  label={p.ownerName}
                  onOpen={() => onOpenResource({ kind: slug, namespace: p.namespace, name: p.ownerName, editable: false })}
                />
              </span>
            )
          },
        }),
        col.accessor('node', {
          header: 'Node',
          cell: (c) => {
            const node = c.getValue()
            if (!node) return <span className="text-xs text-muted-foreground">—</span>
            return (
              <span className="text-xs">
                <ResourceLink label={node} onOpen={() => onOpenResource({ kind: 'node', namespace: '', name: node, editable: false })} />
              </span>
            )
          },
        }),
        col.accessor('age', {
          header: 'Age',
          cell: (c) => <span className="font-mono text-sm text-muted-foreground tabular-nums">{age(c.getValue())}</span>,
          sortingFn: (a, b) => new Date(a.original.age).getTime() - new Date(b.original.age).getTime(),
        }),
      ]
      // Insert the usage mini-gauges as two sortable columns right after Status,
      // when available. Sorting is by utilization % (the number shown, used/ceiling),
      // highest first; pods with no ceiling / no reading get -1 so they sink to the bottom.
      if (hasUsage) {
        const usageCol = (id: string, header: string, kind: 'cores' | 'bytes', basis: UsageBasis, setBasis: (b: UsageBasis) => void) =>
          col.accessor(
            (r) => usageSortValue(r.usage?.[kind === 'cores' ? 'cpu' : 'memory'], basis),
            {
              id,
              header,
              sortDescFirst: true,
              meta: { headerAddon: <UsageBasisToggle basis={basis} onChange={setBasis} /> },
              cell: (c) => {
                const u = c.row.original.usage
                if (!u) return <span className="text-xs text-muted-foreground">—</span>
                return <MiniGauge g={kind === 'cores' ? u.cpu : u.memory} kind={kind} />
              },
            },
          ) as (typeof cols)[number]
        cols.splice(3, 0, usageCol('cpu', 'CPU', 'cores', cpuBasis, setCpuBasis), usageCol('mem', 'Mem', 'bytes', memBasis, setMemBasis))
      }
      return cols as ColumnDef<PodRow, unknown>[]
    },
    [ctx, onOpenResource, hasUsage, cpuBasis, memBasis],
  )

  return (
    <DataTable
      title="Pods"
      data={rows}
      columns={columns}
      headerExtra={<LiveIndicator state={connState} />}
      emptyLabel="Nenhum pod para exibir."
      storageKey="pods"
      facets={['status', 'namespace', 'node']}
      onRowClick={onSelect}
      virtualize
    />
  )
}
