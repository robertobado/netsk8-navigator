import { memo, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Area, CartesianGrid, ComposedChart, Line, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Activity, ChevronLeft, ChevronRight, Cpu, ExternalLink, MemoryStick, Pin, Server } from 'lucide-react'
import { api, type Gauge, type MetricSeries, type NodeUsageItem } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useMetricsRefresh } from '@/lib/metrics'
import { fmtBytes, fmtCores } from '@/lib/usage'
import { useT } from '@/lib/i18n'

const RANGES = ['1h', '6h', '24h'] as const
type Scope = 'cluster' | 'pod' | 'node'

// Respects the global metrics refresh setting: when it's "off", nothing renders
// (the whole metrics area disappears); otherwise the chosen interval drives refetch.
// onOpenNode (cluster scope only) makes each per-node carousel entry link to that
// node's detail drawer.
export function MetricsSection(props: Readonly<{ ctx: string; scope: Scope; namespace?: string; name?: string; onOpenNode?: (name: string) => void }>) {
  const { interval } = useMetricsRefresh()
  if (interval == null) return null
  return <MetricsSectionInner {...props} refreshMs={interval} />
}

// Chooses the richest available monitoring: Prometheus time-series if present,
// else metrics-server instantaneous gauges, else nothing.
function MetricsSectionInner({
  ctx,
  scope,
  namespace,
  name,
  onOpenNode,
  refreshMs,
}: Readonly<{ ctx: string; scope: Scope; namespace?: string; name?: string; onOpenNode?: (name: string) => void; refreshMs: number }>) {
  const monQ = useQuery({ queryKey: ['monitoring', ctx], queryFn: () => api.monitoring(ctx), staleTime: 5 * 60_000, refetchInterval: false })

  if (monQ.data?.available)
    return <TimeSeries ctx={ctx} scope={scope} namespace={namespace} name={name} source={monQ.data.kind} refreshMs={refreshMs} onOpenNode={onOpenNode} />
  if (monQ.data?.metricsServer) return <Gauges ctx={ctx} scope={scope} namespace={namespace} name={name} refreshMs={refreshMs} onOpenNode={onOpenNode} />
  return null
}

function SectionShell({ source, right, children }: Readonly<{ source?: string; right?: React.ReactNode; children: React.ReactNode }>) {
  const t = useT()
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="flex items-center gap-1.5 text-sm font-semibold">
          <Activity className="size-4 text-[color:var(--brand)]" /> {t('Metrics')}
          {source && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-normal uppercase tracking-wider text-muted-foreground">{source}</span>}
        </h3>
        {right}
      </div>
      {children}
    </div>
  )
}

// ---- Instantaneous gauges (metrics-server) --------------------------------
function Gauges({
  ctx,
  scope,
  namespace,
  name,
  onOpenNode,
  refreshMs,
}: Readonly<{ ctx: string; scope: Scope; namespace?: string; name?: string; onOpenNode?: (name: string) => void; refreshMs: number }>) {
  const t = useT()
  const q = useQuery({
    queryKey: ['usage', ctx, scope, namespace, name],
    queryFn: () => api.usage(ctx, scope, { namespace, name }),
    refetchInterval: refreshMs,
  })
  // Per-node gauges only make sense (and only fetched) for the cluster overview.
  const nodesQ = useQuery({
    queryKey: ['nodeusage', ctx],
    queryFn: () => api.nodesUsage(ctx),
    refetchInterval: refreshMs,
    enabled: scope === 'cluster',
  })
  const nodes = scope === 'cluster' && nodesQ.data?.available ? (nodesQ.data.items ?? []) : []

  return (
    <SectionShell source={`metrics-server · ${t('instant')}`}>
      <div className="grid gap-3 md:grid-cols-2">
        <MetricPanel title="CPU" icon={Cpu} kind="cores" g={q.data?.cpu} loading={q.isLoading} nodes={nodes} onOpenNode={onOpenNode} />
        <MetricPanel title={t('Memory')} icon={MemoryStick} kind="bytes" g={q.data?.memory} loading={q.isLoading} nodes={nodes} onOpenNode={onOpenNode} />
      </div>
    </SectionShell>
  )
}

// Shortens an EC2-style node name for the cramped carousel label.
function shortNode(name: string): string {
  return name.replace(/\.ec2\.internal$/, '').replace(/\.compute\.internal$/, '')
}

// A metric panel: the existing overall gauge, plus (cluster scope) a carousel of
// smaller per-node gauges for THIS metric only, sorted by usage % (desc).
function MetricPanel({
  title,
  icon: Icon,
  kind,
  g,
  loading,
  nodes,
  onOpenNode,
}: Readonly<{
  title: string
  icon: typeof Cpu
  kind: 'cores' | 'bytes'
  g?: Gauge
  loading?: boolean
  nodes: NodeUsageItem[]
  onOpenNode?: (name: string) => void
}>) {
  return (
    <div className="rounded-2xl border bg-card/60 p-3 backdrop-blur-xl">
      <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Icon className="size-3.5 text-[color:var(--brand)]" /> {title}
      </div>
      <div className="flex items-stretch gap-4">
        <div className="flex shrink-0 items-center">
          <GaugeCard bare title={nodes.length > 0 ? 'cluster' : ''} icon={Icon} g={g} kind={kind} loading={loading} />
        </div>
        {nodes.length > 0 && <NodeMetricCarousel icon={Icon} kind={kind} nodes={nodes} onOpenNode={onOpenNode} />}
      </div>
    </div>
  )
}

// The node name in a carousel cell: a link into the node's detail drawer when
// onOpenNode is wired up (cluster scope), else plain text.
function NodeNameLabel({ name, onOpenNode }: Readonly<{ name: string; onOpenNode?: (name: string) => void }>) {
  if (!onOpenNode) {
    return (
      <span className="truncate font-mono text-[10px] text-foreground" title={name}>
        {shortNode(name)}
      </span>
    )
  }
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onOpenNode(name)
      }}
      title={name}
      className="group inline-flex min-w-0 items-center gap-0.5 truncate font-mono text-[10px] text-[color:var(--brand)] transition-colors hover:text-foreground"
    >
      <span className="truncate underline decoration-dotted underline-offset-2">{shortNode(name)}</span>
      <ExternalLink className="size-2.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-70" />
    </button>
  )
}

// One per-node gauge cell (name + compact gauge) for the responsive carousel.
function NodeGaugeCell({
  n,
  icon: Icon,
  kind,
  onOpenNode,
}: Readonly<{ n: NodeUsageItem; icon: typeof Cpu; kind: 'cores' | 'bytes'; onOpenNode?: (name: string) => void }>) {
  return (
    <div className="flex min-w-0 flex-col items-center gap-1">
      <div className="flex max-w-full items-center gap-1">
        <Server className="size-3 shrink-0 text-muted-foreground" />
        <NodeNameLabel name={n.name} onOpenNode={onOpenNode} />
      </div>
      <GaugeCard bare compact title="" icon={Icon} g={kind === 'cores' ? n.cpu : n.memory} kind={kind} />
    </div>
  )
}

// Per-node gauges for a single metric, sorted by that metric's % (desc). When the
// gauges overflow the available width they scroll as a continuous conveyor belt
// (paused on hover); otherwise they sit static, centered.
function NodeMetricCarousel({
  icon: Icon,
  kind,
  nodes,
  onOpenNode,
}: Readonly<{ icon: typeof Cpu; kind: 'cores' | 'bytes'; nodes: NodeUsageItem[]; onOpenNode?: (name: string) => void }>) {
  const sorted = useMemo(() => {
    const ratio = (n: NodeUsageItem) => {
      const gg = kind === 'cores' ? n.cpu : n.memory
      return gg.total > 0 ? gg.used / gg.total : -1
    }
    return [...nodes].sort((a, b) => ratio(b) - ratio(a))
  }, [nodes, kind])

  const viewportRef = useRef<HTMLDivElement>(null)
  const groupRef = useRef<HTMLDivElement>(null)
  const [overflow, setOverflow] = useState(false)
  useEffect(() => {
    const vp = viewportRef.current
    const g = groupRef.current
    if (!vp || !g) return
    const check = () => setOverflow(g.scrollWidth > vp.clientWidth + 4)
    const ro = new ResizeObserver(check)
    ro.observe(vp)
    ro.observe(g)
    check()
    return () => ro.disconnect()
  }, [sorted.length])

  const cells = sorted.map((n) => <NodeGaugeCell key={n.name} n={n} icon={Icon} kind={kind} onOpenNode={onOpenNode} />)
  // ~5s per node keeps the belt's speed constant regardless of how many there are.
  const durationS = Math.max(12, sorted.length * 5)

  return (
    <div ref={viewportRef} className="flex min-w-0 flex-1 items-center self-stretch overflow-hidden border-l border-border/50 pl-4">
      {overflow ? (
        <div className="nk-marquee flex w-max shrink-0 items-center" style={{ animationDuration: `${durationS}s` }}>
          <div ref={groupRef} className="flex shrink-0 items-center gap-3 pr-3">
            {cells}
          </div>
          {/* second identical half → seamless wrap */}
          <div className="flex shrink-0 items-center gap-3 pr-3" aria-hidden>
            {sorted.map((n) => (
              <NodeGaugeCell key={`dup-${n.name}`} n={n} icon={Icon} kind={kind} onOpenNode={onOpenNode} />
            ))}
          </div>
        </div>
      ) : (
        <div ref={groupRef} className="flex flex-1 items-center justify-around gap-3">
          {cells}
        </div>
      )}
    </div>
  )
}

// Grafana-style radial gauge geometry: a 270° arc with the gap centered at the
// bottom. Fraction 0→1 maps along the arc from lower-left, clockwise, to lower-right.
const G_SPAN = 270
const G_START = -135
const G_CX = 90
const G_CY = 84
const G_RV = 62 // value-arc radius
const G_RT = 77 // threshold-band radius
function gPoint(r: number, deg: number): [number, number] {
  const a = (deg * Math.PI) / 180
  return [G_CX + r * Math.sin(a), G_CY - r * Math.cos(a)]
}
// Arc path over the fraction range [f0,f1] of the 270° span, at radius r.
function gSeg(r: number, f0: number, f1: number): string {
  const a0 = G_START + f0 * G_SPAN
  const a1 = G_START + f1 * G_SPAN
  const [sx, sy] = gPoint(r, a0)
  const [ex, ey] = gPoint(r, a1)
  return `M ${sx} ${sy} A ${r} ${r} 0 ${a1 - a0 > 180 ? 1 : 0} 1 ${ex} ${ey}`
}
function zoneColor(f: number): string {
  if (f >= 0.9) return 'var(--err)'
  if (f >= 0.8) return 'var(--warn)'
  return 'var(--ok)'
}

// GaugeArc renders the SVG threshold band + value arc + optional request
// marker — pulled out of GaugeCard so its two independent ternaries (bounded
// vs. unbounded band, marker present or not) don't nest inside the card's own.
function GaugeArc({ hasCeiling, vfrac, color, reqMark }: Readonly<{ hasCeiling: boolean; vfrac: number; color: string; reqMark: [number, number][] | null }>) {
  return (
    <>
      {hasCeiling ? (
        <>
          <path d={gSeg(G_RT, 0, 0.8)} stroke="var(--ok)" strokeWidth="4" fill="none" />
          <path d={gSeg(G_RT, 0.8, 0.9)} stroke="var(--warn)" strokeWidth="4" fill="none" />
          <path d={gSeg(G_RT, 0.9, 1)} stroke="var(--err)" strokeWidth="4" fill="none" />
        </>
      ) : (
        <path d={gSeg(G_RT, 0, 1)} stroke="var(--border)" strokeWidth="4" fill="none" strokeDasharray="2 4" />
      )}
      <path d={gSeg(G_RV, 0, 1)} stroke="var(--border)" strokeWidth="12" fill="none" strokeLinecap="round" />
      {hasCeiling && vfrac > 0 && <path d={gSeg(G_RV, 0, vfrac)} stroke={color} strokeWidth="12" fill="none" strokeLinecap="round" />}
      {reqMark && (
        <line x1={reqMark[0][0]} y1={reqMark[0][1]} x2={reqMark[1][0]} y2={reqMark[1][1]} stroke="var(--foreground)" strokeWidth="2.5" strokeLinecap="round" />
      )}
    </>
  )
}

// GaugeLegend is the line below the dial: request/limit when the scope has
// reservations, else the allocatable ceiling, else "no limit set". Three
// mutually-exclusive early returns instead of a nested ternary.
function GaugeLegend({
  compact,
  showReqLim,
  hasCeiling,
  request,
  limit,
  total,
  fmt,
}: Readonly<{
  compact?: boolean
  showReqLim: boolean
  hasCeiling: boolean
  request: number
  limit: number
  total: number
  fmt: (n: number) => string
}>) {
  const t = useT()
  if (compact) return null
  if (showReqLim) {
    return (
      <div className="flex items-center gap-4 text-[11px] text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-0.5 rounded-full bg-foreground" />
          req <span className="font-mono text-foreground">{request > 0 ? fmt(request) : '—'}</span>
        </span>
        <span className="inline-flex items-center gap-1">
          lim <span className="font-mono text-foreground">{limit > 0 ? fmt(limit) : '∞'}</span>
        </span>
      </div>
    )
  }
  if (hasCeiling) {
    return (
      <div className="text-[11px] text-muted-foreground">
        {t('of')} <span className="font-mono text-foreground">{fmt(total)}</span> {t('allocatable')}
      </div>
    )
  }
  return <div className="text-[11px] text-muted-foreground">{t('no limit set')}</div>
}

function GaugeCard({
  title,
  icon: Icon,
  g,
  kind,
  loading,
  bare,
  compact,
}: Readonly<{ title: string; icon: typeof Cpu; g?: Gauge; kind: 'cores' | 'bytes'; loading?: boolean; bare?: boolean; compact?: boolean }>) {
  const dim = compact ? { w: 116, h: 96 } : { w: 148, h: 122 }
  const fmt = kind === 'cores' ? fmtCores : fmtBytes
  const used = g?.used ?? 0
  const request = g?.request ?? 0
  const limit = g?.limit ?? 0
  const total = g?.total ?? 0 // effective ceiling (limit→request for pods; allocatable for node/cluster)
  const hasCeiling = total > 0
  const showReqLim = request > 0 || limit > 0 // pod scope with reservations
  const vfrac = hasCeiling ? Math.min(1, used / total) : 0
  const color = hasCeiling ? zoneColor(vfrac) : 'var(--muted-foreground)'
  // request marker only when a higher limit forms the scale ceiling.
  const reqFrac = hasCeiling && request > 0 && request < total ? request / total : null
  const reqMark = reqFrac != null ? [gPoint(G_RV - 9, G_START + reqFrac * G_SPAN), gPoint(G_RV + 9, G_START + reqFrac * G_SPAN)] : null

  return (
    <div className={cn('flex flex-col items-center', !bare && 'rounded-2xl border bg-card/60 p-3 backdrop-blur-xl')}>
      {title && (
        <div className="flex w-full items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Icon className="size-3.5" style={{ color }} /> {title}
        </div>
      )}
      <div className="relative" style={{ width: dim.w, height: dim.h }}>
        <svg width={dim.w} height={dim.h} viewBox="0 0 180 150">
          <GaugeArc hasCeiling={hasCeiling} vfrac={vfrac} color={color} reqMark={reqMark} />
        </svg>
        <div className="absolute inset-x-0 flex flex-col items-center" style={{ top: '46%', transform: 'translateY(-50%)' }}>
          <span className={cn('font-bold leading-none tabular-nums', compact ? 'text-sm' : 'text-xl')} style={{ color }}>
            {loading ? '…' : fmt(used)}
          </span>
          {hasCeiling && (
            <span className={cn('mt-0.5 font-medium text-muted-foreground tabular-nums', compact ? 'text-[9px]' : 'text-[10px]')}>
              {Math.round(vfrac * 100)}%
            </span>
          )}
        </div>
      </div>
      <GaugeLegend compact={compact} showReqLim={showReqLim} hasCeiling={hasCeiling} request={request} limit={limit} total={total} fmt={fmt} />
    </div>
  )
}

// ---- Prometheus time-series -----------------------------------------------
function TimeSeries({
  ctx,
  scope,
  namespace,
  name,
  source,
  onOpenNode,
  refreshMs,
}: Readonly<{
  ctx: string
  scope: Scope
  namespace?: string
  name?: string
  source?: string
  onOpenNode?: (name: string) => void
  refreshMs: number
}>) {
  const t = useT()
  const [range, setRange] = useState<string>('1h')
  const q = useQuery({
    queryKey: ['metrics', ctx, scope, namespace, name, range],
    queryFn: () => api.metrics(ctx, scope, { namespace, name, range }),
    refetchInterval: refreshMs,
  })
  // The ceiling (allocatable / limit) turns the absolute Y axis into a % scale.
  const ceilQ = useQuery({
    queryKey: ['usage', ctx, scope, namespace, name],
    queryFn: () => api.usage(ctx, scope, { namespace, name }),
    refetchInterval: refreshMs,
  })
  const cpuCeil = ceilQ.data?.cpu?.total ?? 0
  const memCeil = ceilQ.data?.memory?.total ?? 0
  // Per-node list (cluster only) — orders the per-host carousel and gives ceilings.
  const nodesQ = useQuery({
    queryKey: ['nodeusage', ctx],
    queryFn: () => api.nodesUsage(ctx),
    refetchInterval: refreshMs,
    enabled: scope === 'cluster',
  })
  const nodes = scope === 'cluster' && nodesQ.data?.available ? (nodesQ.data.items ?? []) : []

  return (
    <SectionShell
      source={source}
      right={
        <div className="flex gap-1 rounded-lg border bg-card/50 p-0.5">
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className={cn(
                'rounded-md px-2 py-0.5 text-xs font-medium transition-colors',
                range === r ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {r}
            </button>
          ))}
        </div>
      }
    >
      <div className="grid gap-3 lg:grid-cols-2">
        <TimeChartPanel
          title="CPU"
          icon={Cpu}
          kind="cores"
          color="var(--brand)"
          series={q.data?.cpu}
          ceiling={cpuCeil}
          loading={q.isLoading}
          ctx={ctx}
          range={range}
          refreshMs={refreshMs}
          nodes={nodes}
          onOpenNode={onOpenNode}
        />
        <TimeChartPanel
          title={t('Memory')}
          icon={MemoryStick}
          kind="bytes"
          color="var(--primary)"
          series={q.data?.memory}
          ceiling={memCeil}
          loading={q.isLoading}
          ctx={ctx}
          range={range}
          refreshMs={refreshMs}
          nodes={nodes}
          onOpenNode={onOpenNode}
        />
      </div>
    </SectionShell>
  )
}

// A time-series panel: the overall chart on the left, and (cluster scope) a
// carousel of per-host charts on the right, sorted by this metric's % (desc).
function TimeChartPanel({
  title,
  icon: Icon,
  kind,
  color,
  series,
  ceiling = 0,
  loading,
  ctx,
  range,
  refreshMs,
  nodes,
  onOpenNode,
}: Readonly<{
  title: string
  icon: typeof Cpu
  kind: 'cores' | 'bytes'
  color: string
  series?: MetricSeries
  ceiling?: number
  loading?: boolean
  ctx: string
  range: string
  refreshMs: number
  nodes: NodeUsageItem[]
  onOpenNode?: (name: string) => void
}>) {
  const fmt = kind === 'cores' ? fmtCores : fmtBytes
  const last = series?.points?.at(-1)?.v
  const hasPct = ceiling > 0
  const showNodes = nodes.length > 0
  return (
    <div className="rounded-2xl border bg-card/60 p-4 backdrop-blur-xl">
      <div className="mb-3 flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground">
          <Icon className="size-4" style={{ color }} /> {title}
          {showNodes && <span className="text-xs font-normal text-muted-foreground">· cluster</span>}
        </span>
        {last !== undefined && (
          <span className="font-mono text-sm font-semibold tabular-nums">
            {fmt(last)}
            {hasPct && <span className="ml-1.5 text-xs font-normal text-muted-foreground">{Math.round((last / ceiling) * 100)}%</span>}
          </span>
        )}
      </div>
      <div className={cn('gap-3', showNodes && 'grid grid-cols-2')}>
        <Chart title={title} series={series} ceiling={ceiling} color={color} kind={kind} loading={loading} height={140} />
        {showNodes && (
          <NodeChartCarousel title={title} kind={kind} color={color} ctx={ctx} range={range} refreshMs={refreshMs} nodes={nodes} onOpenNode={onOpenNode} />
        )}
      </div>
    </div>
  )
}

// Rotating per-host time-series for a single metric, sorted by that metric's % (desc).
// The current node's series is fetched lazily (and cached) as the carousel lands on it.
function NodeChartCarousel({
  title,
  kind,
  color,
  ctx,
  range,
  refreshMs,
  nodes,
  onOpenNode,
}: Readonly<{
  title: string
  kind: 'cores' | 'bytes'
  color: string
  ctx: string
  range: string
  refreshMs: number
  nodes: NodeUsageItem[]
  onOpenNode?: (name: string) => void
}>) {
  const t = useT()
  const pickG = (n: NodeUsageItem) => (kind === 'cores' ? n.cpu : n.memory)
  const sorted = useMemo(() => {
    const ratio = (n: NodeUsageItem) => {
      const gg = kind === 'cores' ? n.cpu : n.memory
      return gg.total > 0 ? gg.used / gg.total : -1
    }
    return [...nodes].sort((a, b) => ratio(b) - ratio(a))
  }, [nodes, kind])
  const [i, setI] = useState(0)
  const [pinned, setPinned] = useState(false)
  const len = sorted.length
  useEffect(() => {
    if (pinned || len < 2) return
    const id = setInterval(() => setI((v) => v + 1), 5000)
    return () => clearInterval(id)
  }, [pinned, len])

  const idx = len ? ((i % len) + len) % len : 0
  const n = sorted[idx]
  const nodeQ = useQuery({
    queryKey: ['metrics', ctx, 'node', n?.name, range],
    queryFn: () => api.metrics(ctx, 'node', { name: n.name, range }),
    refetchInterval: refreshMs,
    enabled: !!n,
  })
  const series = kind === 'cores' ? nodeQ.data?.cpu : nodeQ.data?.memory
  const ceiling = n ? pickG(n).total : 0
  const go = (d: number) => setI((v) => v + d)

  return (
    <div className="flex flex-col overflow-hidden border-l border-border/50 pl-3">
      <div key={idx} className="nk-slide flex min-w-0 flex-1 flex-col">
        <div className="mb-1 flex items-center gap-1.5">
          <Server className="size-3 shrink-0 text-muted-foreground" />
          {n ? <NodeNameLabel name={n.name} onOpenNode={onOpenNode} /> : <span className="truncate font-mono text-[10px] text-foreground">—</span>}
        </div>
        <Chart title={title} series={series} ceiling={ceiling} color={color} kind={kind} loading={nodeQ.isLoading} height={112} />
      </div>
      {len > 1 && (
        <div className="mt-1 flex items-center gap-1 border-t border-border/50 pt-1.5">
          <button type="button" onClick={() => go(-1)} className="rounded p-0.5 text-muted-foreground hover:text-foreground" aria-label={t('Previous')}>
            <ChevronLeft className="size-3.5" />
          </button>
          <span className="min-w-[2.5rem] text-center text-[10px] tabular-nums text-muted-foreground">
            {idx + 1}/{len}
          </span>
          <button type="button" onClick={() => go(1)} className="rounded p-0.5 text-muted-foreground hover:text-foreground" aria-label={t('Next')}>
            <ChevronRight className="size-3.5" />
          </button>
          <button
            type="button"
            onClick={() => setPinned((v) => !v)}
            title={pinned ? t('Resume carousel') : t('Pin to this node')}
            className={cn('ml-auto rounded p-0.5 transition-colors', pinned ? 'text-[color:var(--brand)]' : 'text-muted-foreground hover:text-foreground')}
          >
            <Pin className={cn('size-3 transition-transform', pinned ? 'rotate-0 fill-current' : 'rotate-45')} />
          </button>
        </div>
      )}
    </div>
  )
}

// Reusable area (absolute) + line (%) chart with aligned dual Y axes.
// Memoized: parent re-renders (carousel ticks, sibling refetches, live ages)
// won't re-render a chart whose data is unchanged — only a new series does.
const Chart = memo(function Chart({
  title,
  series,
  ceiling = 0,
  color,
  kind,
  height = 140,
  loading,
}: Readonly<{ title: string; series?: MetricSeries; ceiling?: number; color: string; kind: 'cores' | 'bytes'; height?: number; loading?: boolean }>) {
  const t = useT()
  const gid = useId()
  // Memoize so the derived domain/data useMemos have a stable dependency
  // (a fresh `?? []` array each render would defeat their memoization).
  const points = useMemo(() => series?.points ?? [], [series])
  const fmt = kind === 'cores' ? fmtCores : fmtBytes
  const hasPct = ceiling > 0
  const domain = useMemo<[number, number]>(() => [0, Math.max(0, ...points.map((p) => p.v)) || 1], [points])
  const data = useMemo(() => points.map((p) => ({ ...p, pct: hasPct ? (p.v / ceiling) * 100 : null })), [points, hasPct, ceiling])

  if (points.length === 0) {
    return (
      <div className="flex items-center justify-center text-xs text-muted-foreground" style={{ height }}>
        {loading ? t('Loading...') : t('No data in this period')}
      </div>
    )
  }
  return (
    <div style={{ height }}>
      <ResponsiveContainer width="100%" height="100%" debounce={200}>
        <ComposedChart data={data} margin={{ top: 4, right: 6, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.35} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke="var(--border)" strokeOpacity={0.4} />
          <XAxis
            dataKey="t"
            tickFormatter={fmtTime}
            minTickGap={48}
            tick={{ fill: 'var(--muted-foreground)', fontSize: 10 }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            yAxisId="abs"
            width={44}
            domain={domain}
            tickFormatter={(v) => fmt(v)}
            tick={{ fill: 'var(--muted-foreground)', fontSize: 10 }}
            axisLine={false}
            tickLine={false}
          />
          {hasPct && (
            <YAxis
              yAxisId="pct"
              orientation="right"
              width={36}
              domain={[0, (max: number) => Math.max(100, Math.ceil(max))]}
              tickFormatter={(v) => `${Math.round(v)}%`}
              tick={{ fill: 'var(--muted-foreground)', fontSize: 10 }}
              axisLine={false}
              tickLine={false}
            />
          )}
          <Tooltip
            cursor={{ stroke: 'var(--border)' }}
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null
              const abs = payload.find((p) => p.dataKey === 'v')?.value
              const pv = payload.find((p) => p.dataKey === 'pct')?.value
              return (
                <div className="rounded-lg border bg-popover/95 px-2.5 py-2 text-xs shadow-2xl shadow-black/40 backdrop-blur-xl">
                  <div className="mb-1 text-[11px] text-muted-foreground tabular-nums">{fmtTime(Number(label))}</div>
                  <div className="flex items-center justify-between gap-5">
                    <span className="flex items-center gap-1.5 text-muted-foreground">
                      <span className="inline-block size-2 rounded-sm" style={{ background: color }} /> {title}
                    </span>
                    <span className="font-mono font-medium tabular-nums">{abs != null ? fmt(Number(abs)) : '—'}</span>
                  </div>
                  {hasPct && pv != null && (
                    <div className="mt-1 flex items-center justify-between gap-5">
                      <span className="flex items-center gap-1.5 text-muted-foreground">
                        <span
                          className="inline-block h-0.5 w-3 rounded-full"
                          style={{ background: 'color-mix(in srgb, var(--foreground) 55%, transparent)' }}
                        />{' '}
                        {t('Utilization')}
                      </span>
                      <span className="font-mono font-medium tabular-nums">{Math.round(Number(pv))}%</span>
                    </div>
                  )}
                </div>
              )
            }}
          />
          <Area
            yAxisId="abs"
            type="monotone"
            dataKey="v"
            name={title}
            stroke={color}
            strokeWidth={2}
            fill={`url(#${gid})`}
            dot={false}
            isAnimationActive={false}
          />
          {hasPct && (
            <Line
              yAxisId="pct"
              type="monotone"
              dataKey="pct"
              name="%"
              stroke="color-mix(in srgb, var(--foreground) 55%, transparent)"
              strokeWidth={1.75}
              dot={false}
              activeDot={{ r: 3, fill: 'var(--foreground)', stroke: 'none' }}
              isAnimationActive={false}
            />
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  )
})

function fmtTime(t: number): string {
  const d = new Date(t * 1000)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}
