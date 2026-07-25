import { useQuery } from '@tanstack/react-query'
import { Boxes, ChevronRight, Layers, Loader2, type LucideIcon } from 'lucide-react'
import { api, type ManifestKind, type Pod } from '@/lib/api'
import { RESOURCES } from '@/lib/resources'

// Human labels for the "Ver detalhes do X" link, keyed by manifest slug.
const KIND_LABEL: Partial<Record<ManifestKind, string>> = {
  deployment: 'deployment',
  statefulset: 'statefulset',
  daemonset: 'daemonset',
  service: 'service',
  serviceaccount: 'service account',
  configmap: 'configmap',
  secret: 'secret',
}

// Inline expansions for the Node and Namespace list rows. Each fetches its own
// join (see backend expansions.go) and renders related resources grouped by
// type, every item clickable to open its detail drawer.

export type OpenTarget = (kind: ManifestKind, namespace: string, name: string) => void

// Icon for a manifest slug, borrowed from the resource catalog so the expansion
// matches the sidebar; falls back to a generic box.
const ICON_BY_SLUG: Record<string, LucideIcon> = Object.fromEntries(RESOURCES.map((r) => [r.manifest, r.icon]))
const iconFor = (slug: string): LucideIcon => ICON_BY_SLUG[slug] ?? Boxes

function ExpansionShell({ children }: Readonly<{ children: React.ReactNode }>) {
  return <div className="ml-1 border-l-2 border-border/60 pl-3">{children}</div>
}

function Loading() {
  return (
    <ExpansionShell>
      <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" /> Carregando...
      </div>
    </ExpansionShell>
  )
}

// A colored dot summarizing a pod's phase (green running, amber pending, red bad).
function podTone(status: string): string {
  if (status === 'Running' || status === 'Succeeded') return 'var(--ok)'
  if (status === 'Pending' || status === 'ContainerCreating' || status === 'PodInitializing' || status === 'Terminating') return 'var(--warn)'
  return 'var(--err)'
}

function PodPill({ pod, onOpen }: Readonly<{ pod: Pod; onOpen: OpenTarget }>) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onOpen('pod', pod.namespace, pod.name)
      }}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs transition-colors hover:bg-accent/40"
    >
      <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: podTone(pod.status) }} />
      <span className="min-w-0 flex-1 truncate font-medium text-[color:var(--brand)]">{pod.name}</span>
      <span className="shrink-0 font-mono text-muted-foreground tabular-nums">{pod.ready}/{pod.total}</span>
      <span className="w-28 shrink-0 truncate text-right text-muted-foreground">{pod.status}</span>
    </button>
  )
}

// A small "open the parent's own detail" affordance, since clicking the row now
// toggles the expansion instead of opening the drawer.
function DetailLink({ label, onClick }: Readonly<{ label: string; onClick: () => void }>) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
      className="mb-2 inline-flex items-center gap-1 text-[11px] font-medium text-[color:var(--brand)] hover:underline"
    >
      {label} <ChevronRight className="size-3" />
    </button>
  )
}

// A titled, scrollable list of pods (shared by the workload / service /
// consumer expansions). Each pod opens its own drawer.
function PodListBody({ title, pods, empty, onOpen }: Readonly<{ title: string; pods: Pod[]; empty: string; onOpen: OpenTarget }>) {
  if (pods.length === 0) return <p className="py-1 text-xs text-muted-foreground">{empty}</p>
  return (
    <>
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
        {title} <span className="tabular-nums text-muted-foreground/50">({pods.length})</span>
      </div>
      <div className="max-h-[26rem] space-y-0.5 overflow-auto pr-1">
        {pods.map((p) => (
          <PodPill key={`${p.namespace}/${p.name}`} pod={p} onOpen={onOpen} />
        ))}
      </div>
    </>
  )
}

// --- Workload / Service → their pods ----------------------------------------

export function WorkloadPodsExpansion({
  ctx,
  kind,
  namespace,
  name,
  onOpen,
}: Readonly<{ ctx: string; kind: ManifestKind; namespace: string; name: string; onOpen: OpenTarget }>) {
  const q = useQuery({ queryKey: ['workloadPods', ctx, kind, namespace, name], queryFn: () => api.workloadPods(ctx, kind, namespace, name) })
  if (q.isLoading) return <Loading />
  const label = KIND_LABEL[kind] ?? kind
  const isSvc = kind === 'service'
  return (
    <ExpansionShell>
      <DetailLink label={`Ver detalhes do ${label}`} onClick={() => onOpen(kind, namespace, name)} />
      <PodListBody
        title={isSvc ? 'Pods de backend' : 'Pods'}
        pods={q.data ?? []}
        empty={isSvc ? 'Nenhum pod corresponde ao selector deste service.' : 'Nenhum pod ativo para este workload.'}
        onOpen={onOpen}
      />
    </ExpansionShell>
  )
}

// --- ConfigMap / Secret → consuming pods ------------------------------------

export function ConsumersExpansion({
  ctx,
  kind,
  namespace,
  name,
  onOpen,
}: Readonly<{ ctx: string; kind: 'configmap' | 'secret'; namespace: string; name: string; onOpen: OpenTarget }>) {
  const q = useQuery({ queryKey: ['consumers', ctx, kind, namespace, name], queryFn: () => api.consumers(ctx, kind, namespace, name) })
  if (q.isLoading) return <Loading />
  return (
    <ExpansionShell>
      <DetailLink label={`Ver detalhes do ${KIND_LABEL[kind] ?? kind}`} onClick={() => onOpen(kind, namespace, name)} />
      <PodListBody title="Consumido por" pods={q.data ?? []} empty="Nenhum pod consome este recurso." onOpen={onOpen} />
    </ExpansionShell>
  )
}

// --- ServiceAccount → bindings + pods running as it -------------------------

export function ServiceAccountExpansion({
  ctx,
  namespace,
  name,
  onOpen,
}: Readonly<{ ctx: string; namespace: string; name: string; onOpen: OpenTarget }>) {
  const q = useQuery({ queryKey: ['saUsage', ctx, namespace, name], queryFn: () => api.serviceAccountUsage(ctx, namespace, name) })
  if (q.isLoading) return <Loading />
  const bindings = q.data?.bindings ?? []
  const pods = q.data?.pods ?? []
  return (
    <ExpansionShell>
      <DetailLink label="Ver detalhes do service account" onClick={() => onOpen('serviceaccount', namespace, name)} />
      {bindings.length === 0 && pods.length === 0 && (
        <p className="py-1 text-xs text-muted-foreground">Nenhum binding referencia esta SA e nenhum pod a usa.</p>
      )}
      {bindings.length > 0 && (
        <div className="mb-2">
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            Bindings <span className="tabular-nums text-muted-foreground/50">({bindings.length})</span>
          </div>
          <div className="space-y-0.5">
            {bindings.map((b) => {
              const Icon = iconFor(b.slug)
              return (
                <button
                  key={`${b.kind}/${b.name}`}
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    onOpen(b.slug, b.namespace, b.name)
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs transition-colors hover:bg-accent/40"
                >
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{b.kind}</span>
                  <span className="min-w-0 flex-1 truncate font-medium text-[color:var(--brand)]">{b.name}</span>
                </button>
              )
            })}
          </div>
        </div>
      )}
      {pods.length > 0 && <PodListBody title="Pods usando esta SA" pods={pods} empty="" onOpen={onOpen} />}
    </ExpansionShell>
  )
}

// --- Node → workloads --------------------------------------------------------

export function NodeExpansion({ ctx, node, onOpen }: Readonly<{ ctx: string; node: string; onOpen: OpenTarget }>) {
  const q = useQuery({ queryKey: ['nodeWorkloads', ctx, node], queryFn: () => api.nodeWorkloads(ctx, node) })
  if (q.isLoading) return <Loading />
  const groups = q.data ?? []
  const totalPods = groups.reduce((n, g) => n + g.pods.length, 0)

  return (
    <ExpansionShell>
      <DetailLink label="Ver detalhes do node" onClick={() => onOpen('node', '', node)} />
      {groups.length === 0 ? (
        <p className="py-1 text-xs text-muted-foreground">Nenhum pod agendado neste node.</p>
      ) : (
        <>
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            Workloads no node <span className="tabular-nums text-muted-foreground/50">({totalPods} pods)</span>
          </div>
          <div className="max-h-[26rem] space-y-2 overflow-auto pr-1">
            {groups.map((g) => {
              const standalone = g.slug === ''
              const Icon = standalone ? Layers : iconFor(g.slug)
              return (
                <div key={`${g.kind}/${g.namespace}/${g.name}`}>
                  <div className="flex items-center gap-1.5 py-0.5">
                    <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{g.kind}</span>
                    {standalone ? (
                      <span className="text-xs text-muted-foreground">Pods avulsos</span>
                    ) : (
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          onOpen(g.slug as ManifestKind, g.namespace, g.name)
                        }}
                        className="min-w-0 truncate text-xs font-medium text-[color:var(--brand)] hover:underline"
                      >
                        {g.namespace}/{g.name}
                      </button>
                    )}
                    <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground/60 tabular-nums">{g.pods.length}</span>
                  </div>
                  <div className="ml-2 border-l border-border/40 pl-2">
                    {g.pods.map((p) => (
                      <PodPill key={`${p.namespace}/${p.name}`} pod={p} onOpen={onOpen} />
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
    </ExpansionShell>
  )
}

// --- Namespace → resources by type ------------------------------------------

export function NamespaceExpansion({ ctx, ns, onOpen }: Readonly<{ ctx: string; ns: string; onOpen: OpenTarget }>) {
  const q = useQuery({ queryKey: ['namespaceSummary', ctx, ns], queryFn: () => api.namespaceSummary(ctx, ns) })
  if (q.isLoading) return <Loading />
  const groups = q.data ?? []
  const total = groups.reduce((n, g) => n + g.items.length, 0)

  return (
    <ExpansionShell>
      <DetailLink label="Ver detalhes do namespace" onClick={() => onOpen('namespace', '', ns)} />
      {groups.length === 0 ? (
        <p className="py-1 text-xs text-muted-foreground">Nenhum recurso neste namespace.</p>
      ) : (
        <>
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            Recursos no namespace <span className="tabular-nums text-muted-foreground/50">({total})</span>
          </div>
          <div className="max-h-[26rem] space-y-2 overflow-auto pr-1">
            {groups.map((g) => {
              const Icon = iconFor(g.slug)
              return (
                <div key={g.kind}>
                  <div className="flex items-center gap-1.5 py-0.5">
                    <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="text-xs font-medium">{g.kind}</span>
                    <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground/60 tabular-nums">{g.items.length}</span>
                  </div>
                  <div className="ml-5 flex flex-wrap gap-1">
                    {g.items.map((it) => (
                      <button
                        key={it.name}
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          onOpen(g.slug, it.namespace, it.name)
                        }}
                        className="max-w-[16rem] truncate rounded-md bg-muted/60 px-2 py-0.5 text-[11px] text-[color:var(--brand)] transition-colors hover:bg-accent/60"
                        title={it.name}
                      >
                        {it.name}
                      </button>
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
    </ExpansionShell>
  )
}
