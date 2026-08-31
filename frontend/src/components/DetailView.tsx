import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronRight, Eye, EyeOff, ExternalLink, Loader2, Plug } from 'lucide-react'
import { api, getDetail, kindToSlug, type DetailChip, type DetailKV, type ManifestKind, type Pod, type ResourceDetail } from '@/lib/api'
import { age, cn, openExternal } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { MetricsSection } from './MetricsSection'
import { StatusBadge } from './StatusBadge'

// Kinds whose detail lists their backing pods (workloads by ownership; a Service
// by selector).
const POD_LISTING_KINDS = new Set<ManifestKind>(['deployment', 'replicaset', 'statefulset', 'daemonset', 'job', 'service'])

const TONE: Record<string, string> = {
  ok: 'text-[color:var(--ok)]',
  warn: 'text-[color:var(--warn)]',
  err: 'text-[color:var(--err)]',
  muted: 'text-muted-foreground',
}
const PILL: Record<string, string> = {
  ok: 'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25',
  warn: 'bg-[color:var(--warn)]/12 text-[color:var(--warn)] ring-[color:var(--warn)]/25',
  err: 'bg-[color:var(--err)]/12 text-[color:var(--err)] ring-[color:var(--err)]/25',
  muted: 'bg-muted text-muted-foreground ring-border',
}

// Structured resource detail (nicer than a raw YAML dump). `onOpenPod`, when
// provided for a workload kind, adds a clickable list of the workload's pods.
interface DetailBodyProps {
  d: ResourceDetail
  ctx: string
  kind: string
  namespace: string
  name: string
  onOpenPod?: (p: Pod) => void
  onOpenResource?: (t: { kind: ManifestKind; namespace: string; name: string }) => void
}

export function DetailView({
  ctx,
  kind,
  namespace,
  name,
  onOpenPod,
  onOpenResource,
}: Readonly<{
  ctx: string
  kind: ManifestKind
  namespace: string
  name: string
  onOpenPod?: (p: Pod) => void
  onOpenResource?: (t: { kind: ManifestKind; namespace: string; name: string }) => void
}>) {
  const t = useT()
  const q = useQuery({
    queryKey: ['detail', ctx, kind, namespace, name],
    queryFn: () => getDetail(ctx, kind, namespace, name),
  })

  if (q.isLoading)
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> {t('Loading details...')}
      </div>
    )
  if (q.isError || !q.data)
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-sm text-[color:var(--err)]">{(q.error as Error)?.message ?? t('Error')}</div>
    )

  return <DetailBody d={q.data} ctx={ctx} kind={kind} namespace={namespace} name={name} onOpenPod={onOpenPod} onOpenResource={onOpenResource} />
}

// Presentational detail (shared by typed resources and CRDs).
export function DetailBody({ d, ctx, kind, namespace, name, onOpenPod, onOpenResource }: Readonly<DetailBodyProps>) {
  const t = useT()
  // Go returns nil slices as null — coalesce so `.length`/`.map` are safe.
  const status = d.status ?? []
  const sections = d.sections ?? []
  const images = d.images ?? []
  const conditions = d.conditions ?? []
  const labels = d.labels ? Object.entries(d.labels) : []
  const selector = d.selector ? Object.entries(d.selector) : []
  const refs = d.refs ?? []
  const blocks = d.blocks ?? []
  const hosts = d.hosts ?? []
  const ports = d.ports ?? []
  const refGroups = [...new Set(refs.map((r) => r.group))]
  const ownerSlug = d.ownerKind ? kindToSlug(d.ownerKind) : null

  return (
    <div className="h-full space-y-5 overflow-y-auto p-5">
      {/* Status tiles */}
      {status.length > 0 && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {status.map((c) => (
            <div key={c.label} className="rounded-xl border bg-card/60 px-3 py-2.5">
              <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{t(c.label)}</div>
              <div className={cn('mt-0.5 truncate text-lg font-semibold tabular-nums', TONE[c.tone])}>{t(c.value) || '—'}</div>
            </div>
          ))}
        </div>
      )}

      {/* Problem banner — same reason/message the overview's issue carousels show */}
      {d.problem && (
        <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-card/60 px-3 py-2.5">
          <span className={cn('shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ring-inset', PILL[d.problem.tone])}>{t(d.problem.reason)}</span>
          <span className="min-w-0 flex-1 text-xs text-muted-foreground" title={d.problem.message}>
            {d.problem.message || t('no detail')}
          </span>
        </div>
      )}

      {/* Meta line */}
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
        {d.namespace && (
          <span>
            Namespace: <span className="text-foreground">{d.namespace}</span>
          </span>
        )}
        <span>
          {t('Age')}: <span className="text-foreground">{age(d.age)}</span>
        </span>
        {d.ownerKind && (
          <span>
            {t('Controlled by')}:{' '}
            {ownerSlug && onOpenResource ? (
              <button
                type="button"
                onClick={() => onOpenResource({ kind: ownerSlug, namespace: d.namespace, name: d.ownerName })}
                className="font-medium text-[color:var(--brand)] underline decoration-dotted underline-offset-2 transition-colors hover:text-foreground"
                title={t('Open owner')}
              >
                {d.ownerKind}/{d.ownerName}
              </button>
            ) : (
              <span className="text-foreground">
                {d.ownerKind}/{d.ownerName}
              </span>
            )}
          </span>
        )}
      </div>

      {/* Live metrics (only when a metrics backend is available) */}
      {(kind === 'pod' || kind === 'node') && <MetricsSection ctx={ctx} scope={kind} namespace={kind === 'pod' ? namespace : undefined} name={name} />}

      {/* Backing pods (workload by ownership; service by selector) */}
      {onOpenPod && POD_LISTING_KINDS.has(kind as ManifestKind) && (
        <WorkloadPods
          ctx={ctx}
          kind={kind as ManifestKind}
          namespace={namespace}
          name={name}
          onOpen={onOpenPod}
          label={kind === 'service' ? 'Endpoints' : 'Pods'}
        />
      )}

      {/* Hosts (routes) — non-wildcard hosts open in a new tab */}
      {hosts.length > 0 && (
        <Card title={t('Hosts')}>
          <div className="space-y-0.5">
            {hosts.map((h) => {
              const wildcard = h.includes('*')
              if (wildcard)
                return (
                  <div key={h} className="px-2 py-1.5 font-mono text-sm text-muted-foreground">
                    {h}
                  </div>
                )
              return (
                <a
                  key={h}
                  href={`https://${h}`}
                  target="_blank"
                  rel="noreferrer"
                  className="group flex items-center gap-2 rounded-lg px-2 py-1.5 transition-colors hover:bg-accent/40"
                  onClick={(e) => {
                    // See openExternal's doc comment — target="_blank" alone
                    // does nothing in the desktop app.
                    e.preventDefault()
                    void openExternal(`https://${h}`)
                  }}
                >
                  <span className="min-w-0 flex-1 truncate font-mono text-sm text-[color:var(--brand)] underline decoration-dotted underline-offset-2">
                    {h}
                  </span>
                  <ExternalLink className="size-3.5 shrink-0 text-muted-foreground opacity-60 transition-opacity group-hover:opacity-100" />
                </a>
              )
            })}
          </div>
        </Card>
      )}

      {/* Images */}
      {images.length > 0 && (
        <Card title={t('Images')}>
          <div className="space-y-1.5">
            {images.map((im) => (
              <div key={im.label} className="flex flex-col gap-0.5 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
                <span className="shrink-0 text-sm font-medium">{im.label}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">{im.value}</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      {/* Ports — as pills */}
      {ports.length > 0 && (
        <Card title={t('Ports')}>
          <div className="flex flex-wrap gap-2">
            {ports.map((p) => (
              <span key={`${p.name}-${p.port}-${p.protocol}`} className="inline-flex items-center gap-2 rounded-lg border bg-background/40 py-1 pl-2 pr-2.5">
                <Plug className="size-3.5 shrink-0 text-[color:var(--brand)]" />
                {p.name && <span className="text-xs font-medium text-muted-foreground">{p.name}</span>}
                <span className="font-mono text-sm font-semibold tabular-nums">{p.port}</span>
                {p.protocol && (
                  <span className="rounded bg-muted px-1 py-0.5 text-[9px] font-medium uppercase tracking-wider text-muted-foreground">{p.protocol}</span>
                )}
                {p.extra && <span className="font-mono text-[10px] text-muted-foreground">{p.extra}</span>}
              </span>
            ))}
          </div>
        </Card>
      )}

      {/* Sections */}
      <div className="grid gap-3 sm:grid-cols-2">
        {sections.map((s) => (
          <Card key={s.title} title={t(s.title)}>
            <div className="space-y-1.5">
              {s.items.map((it) => (
                <KVRow key={it.label} item={it} />
              ))}
            </div>
          </Card>
        ))}
      </div>

      {/* Cross-links (e.g. an Ingress's backend services) */}
      {onOpenResource &&
        refGroups.map((group) => (
          <Card key={group} title={t(group)}>
            <div className="space-y-0.5">
              {refs
                .filter((r) => r.group === group)
                .map((r) => (
                  <button
                    type="button"
                    key={`${r.kind}/${r.namespace}/${r.name}`}
                    onClick={() => onOpenResource({ kind: r.kind, namespace: r.namespace, name: r.name })}
                    className="group flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/40"
                    title={t('Open details')}
                  >
                    <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                      {r.kind}
                    </span>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-sm font-medium text-[color:var(--brand)] underline decoration-dotted underline-offset-2">{r.name}</span>
                      {r.namespace && <span className="truncate font-mono text-[11px] text-muted-foreground">{r.namespace}</span>}
                    </span>
                    {r.note && (
                      <span
                        className={cn('shrink-0 font-mono text-[11px]', r.note.includes('not ready') ? 'text-[color:var(--warn)]' : 'text-muted-foreground')}
                      >
                        {r.note
                          .split(' · ')
                          .map((seg) => t(seg))
                          .join(' · ')}
                      </span>
                    )}
                    <ChevronRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-70" />
                  </button>
                ))}
            </div>
          </Card>
        ))}

      {/* Content blocks (e.g. ConfigMap keys) — compact key/value rows */}
      {blocks.length > 0 && (
        <Card title={t('Data')}>
          <div className="divide-y divide-border/40">
            {blocks.map((b) => (
              <DataRow key={b.title} title={b.title} body={b.body} masked={b.masked} />
            ))}
          </div>
        </Card>
      )}

      {/* Conditions */}
      {conditions.length > 0 && (
        <Card title={t('Conditions')}>
          <div className="flex flex-wrap gap-2">
            {conditions.map((c) => (
              <ConditionPill key={c.label} chip={c} />
            ))}
          </div>
        </Card>
      )}

      {/* Selector */}
      {selector.length > 0 && (
        <Card title="Selector">
          <div className="flex flex-wrap gap-1.5">
            {selector.map(([k, v]) => (
              <Chip key={k} text={`${k}=${v}`} />
            ))}
          </div>
        </Card>
      )}

      {/* Labels */}
      {labels.length > 0 && (
        <Card title="Labels">
          <div className="flex flex-wrap gap-1.5">
            {labels.map(([k, v]) => (
              <Chip key={k} text={v ? `${k}=${v}` : k} />
            ))}
          </div>
        </Card>
      )}
    </div>
  )
}

function WorkloadPods({
  ctx,
  kind,
  namespace,
  name,
  onOpen,
  label = 'Pods',
}: Readonly<{ ctx: string; kind: ManifestKind; namespace: string; name: string; onOpen: (p: Pod) => void; label?: string }>) {
  const t = useT()
  const q = useQuery({
    queryKey: ['workloadpods', ctx, kind, namespace, name],
    queryFn: () => api.workloadPods(ctx, kind, namespace, name),
    refetchInterval: 10_000,
  })
  const pods = q.data ?? []

  let body: ReactNode
  if (q.isLoading) {
    body = (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" /> {t('Loading pods...')}
      </div>
    )
  } else if (pods.length === 0) {
    body = <div className="text-xs text-muted-foreground">{t('No pods.')}</div>
  } else {
    body = (
      <div className="space-y-0.5">
        {pods.map((p) => (
          <button
            type="button"
            key={`${p.namespace}/${p.name}`}
            onClick={() => onOpen(p)}
            className="group flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/40"
            title={t('Open pod details')}
          >
            <span className="min-w-0 flex-1 truncate text-sm font-medium text-[color:var(--brand)] underline decoration-dotted underline-offset-2">
              {p.name}
            </span>
            <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
              {p.ready}/{p.total}
            </span>
            <StatusBadge status={p.reason || p.status} />
            <ChevronRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-70" />
          </button>
        ))}
      </div>
    )
  }

  return <Card title={`${t(label)} (${pods.length})`}>{body}</Card>
}

// A ConfigMap-style key/value row: short scalars inline; long/multiline values
// collapse to a single preview line and expand on click.
function DataRow({ title, body, masked }: Readonly<{ title: string; body: string; masked?: boolean }>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [revealed, setRevealed] = useState(false)

  // Secret values stay hidden until explicitly revealed (per key).
  if (masked) {
    return (
      <div className="py-1.5">
        <div className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate font-mono text-xs font-medium">{title}</span>
          <button
            type="button"
            onClick={() => setRevealed((r) => !r)}
            title={revealed ? t('Hide value') : t('Reveal value')}
            className="inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            {revealed ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
            {revealed ? t('Hide') : t('Reveal')}
          </button>
        </div>
        {revealed ? (
          <pre className="mt-1.5 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-background/50 p-2.5 font-mono text-xs leading-relaxed text-foreground/90">
            {body}
          </pre>
        ) : (
          <div className="mt-1 select-none font-mono text-xs tracking-widest text-muted-foreground/60">••••••••••••</div>
        )}
      </div>
    )
  }

  const multiline = body.includes('\n') || body.length > 80

  if (!multiline) {
    return (
      <div className="flex items-baseline justify-between gap-4 py-1.5">
        <span className="shrink-0 font-mono text-xs text-muted-foreground">{title}</span>
        <span className="truncate text-right font-mono text-xs">{body || '—'}</span>
      </div>
    )
  }
  return (
    <div className="py-1.5">
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 text-left">
        <ChevronRight className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} />
        <span className="shrink-0 font-mono text-xs font-medium">{title}</span>
        {!open && <span className="min-w-0 flex-1 truncate text-right font-mono text-[11px] text-muted-foreground">{body.split('\n')[0]}</span>}
      </button>
      {open && (
        <pre className="mt-1.5 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-background/50 p-2.5 font-mono text-xs leading-relaxed text-foreground/90">
          {body}
        </pre>
      )}
    </div>
  )
}

function Card({ title, children }: Readonly<{ title: string; children: React.ReactNode }>) {
  return (
    <div className="rounded-xl border bg-card/50 p-3.5">
      <h3 className="mb-2.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground/80">{title}</h3>
      {children}
    </div>
  )
}

function Chip({ text }: Readonly<{ text: string }>) {
  return <span className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground">{text}</span>
}

// One section field: a flat label/value row, a simple array as chips, a
// nested mini-grid (object whose own fields are all simple), or a read-only
// YAML code block for anything nested deeper — see DetailKV in lib/api.ts.
function KVRow({ item }: Readonly<{ item: DetailKV }>) {
  const t = useT()

  if (item.grid && item.grid.length > 0) {
    return (
      <div className="py-0.5">
        <div className="mb-1 text-xs font-medium">{t(item.label)}</div>
        <div className="space-y-1 rounded-lg border bg-background/40 p-2.5">
          {item.grid.map((g) => (
            <KVRow key={g.label} item={g} />
          ))}
        </div>
      </div>
    )
  }

  if (item.code) {
    return (
      <div className="py-0.5">
        <div className="mb-1 flex items-center gap-2">
          <span className="text-xs font-medium">{t(item.label)}</span>
          <span className="rounded bg-muted px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wider text-muted-foreground">yaml</span>
        </div>
        <pre className="overflow-x-auto rounded-lg border bg-background/60 p-2.5 font-mono text-xs leading-relaxed text-foreground/90">{item.code}</pre>
      </div>
    )
  }

  if (item.chips && item.chips.length > 0) {
    return (
      <div className="flex items-baseline justify-between gap-4 py-0.5">
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{t(item.label)}</span>
        <div className="flex min-w-0 flex-1 flex-wrap justify-end gap-1">
          {item.chips.map((c) => (
            <Chip key={c} text={c} />
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="flex items-baseline justify-between gap-4 py-0.5">
      <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{t(item.label)}</span>
      <span className="min-w-0 flex-1 break-words text-right text-sm">{item.value || '—'}</span>
    </div>
  )
}

function ConditionPill({ chip }: Readonly<{ chip: DetailChip }>) {
  const t = useT()
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', PILL[chip.tone])}>
      {t(chip.label)}
      <span className="opacity-70">{t(chip.value)}</span>
    </span>
  )
}
