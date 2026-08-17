import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import { Plus, RefreshCw, ServerCrash } from 'lucide-react'
import { api, kindToSlug, CREATABLE_KINDS, type CreatedResource, type ManifestKind, type PodUsageEntry } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import type { ResourceDef, ResourceExpand } from '@/lib/resources'
import { DataTable } from './DataTable'
import { useMetricsRefresh } from '@/lib/metrics'
import { MiniGauge, UsageBasisToggle } from './UsageGauge'
import { usageSortValue, readBasis, writeBasis, type UsageBasis } from '@/lib/usage'
import { ResourceDrawer, type DrawerTarget } from './ResourceDrawer'
import { CreateResourceDialogLazy } from './CreateResourceDialogLazy'
import { NodeExpansion, NamespaceExpansion, WorkloadPodsExpansion, ServiceAccountExpansion, ConsumersExpansion } from './ResourceExpansions'

type Row = { name: string; namespace: string }
type UsageRow = Row & { usage?: PodUsageEntry }

// Sortable CPU/Mem mini-gauge columns for a workload (aggregated from its pods),
// mirroring the pods table. Basis (%/abs) is chosen per column via the header toggle.
const uc = createColumnHelper<UsageRow>()
function usageColumns(cpuBasis: UsageBasis, setCpu: (b: UsageBasis) => void, memBasis: UsageBasis, setMem: (b: UsageBasis) => void) {
  const build = (id: string, header: string, mkind: 'cores' | 'bytes', basis: UsageBasis, setBasis: (b: UsageBasis) => void) =>
    uc.accessor((r) => usageSortValue(r.usage?.[mkind === 'cores' ? 'cpu' : 'memory'], basis), {
      id,
      header,
      sortDescFirst: true,
      meta: { headerAddon: <UsageBasisToggle basis={basis} onChange={setBasis} /> },
      cell: (c) => {
        const u = c.row.original.usage
        if (!u) return <span className="text-xs text-muted-foreground">—</span>
        return <MiniGauge g={mkind === 'cores' ? u.cpu : u.memory} kind={mkind} />
      },
    }) as ColumnDef<never, unknown>
  return [build('cpu', 'CPU', 'cores', cpuBasis, setCpu), build('mem', 'Mem', 'bytes', memBasis, setMem)]
}

// Injected count column for a parent→children expansion (StorageClass → PVs).
const cec = createColumnHelper<Row>()
function countColumn(expand: ResourceExpand, group: Map<string, Row[]>) {
  return cec.accessor((r) => group.get(expand.parentKey(r as Record<string, unknown>))?.length ?? 0, {
    id: '__childcount',
    header: expand.countHeader,
    sortDescFirst: true,
    cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue() as number}</span>,
  }) as ColumnDef<never, unknown>
}

// Generic list view driven by a catalog entry (see lib/resources).
export function ResourceView({ def, ctx, ns }: Readonly<{ def: ResourceDef; ctx: string; ns: string }>) {
  const t = useT()
  const [target, setTarget] = useState<DrawerTarget | null>(null)
  const [creating, setCreating] = useState(false)
  const q = useQuery({
    queryKey: ['resources', def.resource, ctx, ns],
    queryFn: () => api.list(ctx, def.resource, ns || undefined),
    enabled: !!ctx,
  })

  // Workloads with usage:true get aggregated per-item CPU/mem gauges.
  const { interval } = useMetricsRefresh()
  const usageQ = useQuery({
    queryKey: ['deploymentusage', ctx, ns],
    queryFn: () => api.deploymentsUsage(ctx, ns || undefined),
    enabled: !!def.usage && interval != null,
    refetchInterval: interval ?? false,
  })
  const usage = def.usage && interval != null && usageQ.data?.available ? usageQ.data.items : undefined
  const hasUsage = !!usage
  const [cpuBasis, setCpuBasis] = useState<UsageBasis>(() => readBasis('dep-cpu'))
  const [memBasis, setMemBasis] = useState<UsageBasis>(() => readBasis('dep-mem'))
  useEffect(() => writeBasis('dep-cpu', cpuBasis), [cpuBasis])
  useEffect(() => writeBasis('dep-mem', memBasis), [memBasis])

  // Parent→children expansion (StorageClass → PVs): fetch the child resource and
  // group it by the parent key, for a count column + an expandable child list.
  const expand = def.expand
  const childQ = useQuery({
    queryKey: ['expand', def.key, expand?.resource, ctx],
    queryFn: () => api.list(ctx, expand!.resource),
    enabled: !!expand && !!ctx,
  })
  const childrenByParent = useMemo(() => {
    const m = new Map<string, Row[]>()
    if (!expand) return m
    for (const c of (childQ.data ?? []) as Record<string, unknown>[]) {
      const k = expand.childKey(c)
      if (!k) continue
      const arr = m.get(k) ?? []
      arr.push(c as unknown as Row)
      m.set(k, arr)
    }
    return m
  }, [expand, childQ.data])

  // Revision history (ReplicaSets): show only the current revision per controller;
  // clicking it expands the row to reveal that controller's older revisions.
  const history = def.history
  const open = (r: Row) => setTarget({ kind: def.manifest as ManifestKind, namespace: r.namespace, name: r.name })

  // All revisions grouped by controller (for the expansion), sorted newest first.
  const revsByGroup = useMemo(() => {
    const m = new Map<string, Row[]>()
    if (!history) return m
    for (const r of (q.data ?? []) as Row[]) {
      const g = history.groupKey(r as Record<string, unknown>)
      const arr = m.get(g) ?? []
      arr.push(r)
      m.set(g, arr)
    }
    for (const arr of m.values()) arr.sort((a, b) => history.revision(b as Record<string, unknown>) - history.revision(a as Record<string, unknown>))
    return m
  }, [q.data, history])

  // Attach usage to each row so the table re-sorts on every metrics refetch.
  const rows = useMemo(() => {
    const base = (q.data ?? []) as Row[]
    const withUsage: Row[] = usage ? base.map((d) => ({ ...d, usage: usage[`${d.namespace}/${d.name}`] })) : base
    // History resources show only the current revision in the table.
    return history ? withUsage.filter((r) => !history.isOld(r as Record<string, unknown>)) : withUsage
  }, [q.data, usage, history])

  const columns = useMemo(() => {
    const cols = [...def.columns]
    if (hasUsage) cols.splice(2, 0, ...usageColumns(cpuBasis, setCpuBasis, memBasis, setMemBasis))
    if (expand) cols.push(countColumn(expand, childrenByParent))
    return cols
  }, [def.columns, hasUsage, cpuBasis, memBasis, expand, childrenByParent])

  let expandable: ((row: Row) => ReactNode | null) | undefined
  if (history) {
    expandable = (row: Row) => {
      const revs = revsByGroup.get(history.groupKey(row as Record<string, unknown>)) ?? []
      const older = revs.filter((r) => history.isOld(r as Record<string, unknown>))
      return older.length > 0 ? <RevisionHistory revs={revs} onOpen={open} /> : null
    }
  } else if (expand) {
    expandable = (row: Row) => {
      const kids = childrenByParent.get(expand.parentKey(row as Record<string, unknown>)) ?? []
      if (kids.length === 0) return null
      const openChild = (k: Row) => setTarget({ kind: expand.manifest, namespace: k.namespace || '', name: k.name })
      return <ChildList title={expand.title} kids={kids} render={expand.renderChild} onOpen={openChild} />
    }
  } else if (def.customExpand) {
    const ce = def.customExpand
    const open2 = (kind: ManifestKind, namespace: string, name: string) => setTarget({ kind, namespace, name })
    const kind = def.manifest as ManifestKind
    expandable = (row: Row) => {
      switch (ce) {
        case 'node':
          return <NodeExpansion ctx={ctx} node={row.name} onOpen={open2} />
        case 'namespace':
          return <NamespaceExpansion ctx={ctx} ns={row.name} onOpen={open2} />
        case 'serviceaccount':
          return <ServiceAccountExpansion ctx={ctx} namespace={row.namespace} name={row.name} onOpen={open2} />
        case 'consumers':
          return <ConsumersExpansion ctx={ctx} kind={kind as 'configmap' | 'secret'} namespace={row.namespace} name={row.name} onOpen={open2} />
        default:
          return <WorkloadPodsExpansion ctx={ctx} kind={kind} namespace={row.namespace} name={row.name} onOpen={open2} />
      }
    }
  }

  // Surface list failures instead of rendering an empty table (which reads as
  // "no resources" when it's really a cluster/credential error).
  if (q.isError) {
    const raw = (q.error as Error).message
    const auth = /credential|exec:|Unauthorized|forbidden|token/i.test(raw)
    return (
      <div className="flex h-56 flex-col items-center justify-center gap-2 rounded-2xl border bg-card/60 text-center backdrop-blur-xl">
        <ServerCrash className="size-6 text-[color:var(--err)]" />
        <p className="text-sm font-medium text-[color:var(--err)]">
          {t('Could not load')} {def.label}.
        </p>
        <p className="max-w-md text-xs text-muted-foreground">
          {auth
            ? t(
                'The connection to the Kubernetes API failed — expired credential or no permission. Renew the cluster login (e.g. AWS credentials) and try again.',
              )
            : t('The Kubernetes API did not respond to this listing.')}
        </p>
        <p className="max-w-lg truncate px-4 font-mono text-[10px] text-muted-foreground/60" title={raw}>
          {raw}
        </p>
        <button
          type="button"
          onClick={() => q.refetch()}
          disabled={q.isFetching}
          className="mt-1 inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors hover:bg-accent disabled:opacity-50"
        >
          <RefreshCw className={cn('size-3.5', q.isFetching && 'animate-spin')} /> {t('Try again')}
        </button>
      </div>
    )
  }

  const creatable = CREATABLE_KINDS.has(def.manifest)

  return (
    <>
      <DataTable
        title={def.label}
        data={rows as never[]}
        columns={columns}
        loading={q.isLoading}
        storageKey={def.key}
        facets={def.facets}
        expandable={expandable as ((row: never) => ReactNode | null) | undefined}
        onRowClick={(row) => open(row as Row)}
        virtualize={!expandable}
        headerExtra={
          creatable ? (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <Plus className="size-3.5" /> {t('New')}
            </button>
          ) : undefined
        }
      />
      <ResourceDrawer target={target} ctx={ctx} onClose={() => setTarget(null)} />
      {creatable && (
        <CreateResourceDialogLazy
          ctx={ctx}
          kind={def.manifest}
          namespace={ns}
          clusterScoped={!!def.clusterScoped}
          open={creating}
          onClose={() => setCreating(false)}
          onCreated={(result: CreatedResource) => {
            setCreating(false)
            q.refetch()
            const slug = kindToSlug(result.kind)
            if (slug) setTarget({ kind: slug, namespace: result.namespace, name: result.name })
          }}
        />
      )}
    </>
  )
}

// The list of related children (e.g. a StorageClass's PVs), shown when its row
// expands. Each child is clickable to open its own detail drawer.
function ChildList({
  title,
  kids,
  render,
  onOpen,
}: Readonly<{ title: string; kids: Row[]; render: (c: Record<string, unknown>) => ReactNode; onOpen: (k: Row) => void }>) {
  const t = useT()
  return (
    <div className="ml-1 border-l-2 border-border/60 pl-3">
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
        {t(title)} <span className="tabular-nums text-muted-foreground/50">({kids.length})</span>
      </div>
      <div className="max-h-96 space-y-0.5 overflow-auto pr-1">
        {kids.map((k) => (
          <button
            key={k.name}
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onOpen(k)
            }}
            className="flex w-full items-center gap-3 rounded-md px-2 py-1 text-left text-xs transition-colors hover:bg-accent/40"
          >
            {render(k as unknown as Record<string, unknown>)}
          </button>
        ))}
      </div>
    </div>
  )
}

// The controller's revision history, shown when a current ReplicaSet row expands.
function RevisionHistory({ revs, onOpen }: Readonly<{ revs: Row[]; onOpen: (r: Row) => void }>) {
  const t = useT()
  return (
    <div className="ml-1 border-l-2 border-border/60 pl-3">
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">{t('Revision history')}</div>
      <div className="space-y-0.5">
        {revs.map((r) => {
          const rev = r as unknown as { name: string; revision: string; ready: string; age: string; current: boolean }
          return (
            <button
              key={rev.name}
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                onOpen(r)
              }}
              className="flex w-full items-center gap-3 rounded-md px-2 py-1 text-left text-xs transition-colors hover:bg-accent/40"
            >
              <span className="w-8 shrink-0 font-mono text-muted-foreground tabular-nums">r{rev.revision || '?'}</span>
              <span className="min-w-0 flex-1 truncate font-medium">{rev.name}</span>
              {rev.current && (
                <span className="shrink-0 rounded-full bg-[color:var(--ok)]/12 px-1.5 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">
                  {t('current')}
                </span>
              )}
              <span className="shrink-0 font-mono text-muted-foreground tabular-nums">{rev.ready}</span>
              <span className="w-10 shrink-0 text-right font-mono text-muted-foreground tabular-nums">{age(rev.age)}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
