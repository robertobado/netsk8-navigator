import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, Boxes, CircleAlert, CircleDot, Layers, Menu, Server } from 'lucide-react'
import { api } from '@/lib/api'
import { useLivePods } from '@/lib/useLivePods'
import { cn, shortContext } from '@/lib/utils'
import { ContextSwitcher } from '@/components/ContextSwitcher'
import { NamespaceSelect } from '@/components/NamespaceSelect'
import { ResourceNav } from '@/components/ResourceNav'
import { crdKindView, crdView, type View } from '@/lib/nav'
import { CustomResourceView } from '@/components/CustomResourceView'
import { CRDKindView } from '@/components/CRDKindView'
import { StatCard } from '@/components/StatCard'
import { PodsTable } from '@/components/PodsTable'
import { PodDrawer } from '@/components/PodDrawer'
import { ResourceView } from '@/components/ResourceView'
import { resourceByKey } from '@/lib/resources'
import { ResourceDrawer, type DrawerTarget } from '@/components/ResourceDrawer'
import { TopologyView } from '@/components/TopologyView'
import { EventsView } from '@/components/EventsView'
import { HelmView } from '@/components/HelmView'
import { FloatingBubble } from '@/components/FloatingBubble'
import { CommandPalette } from '@/components/CommandPalette'
import { NavigatorLoader, LoaderPreview } from '@/components/Loader'
import { MetricsSection } from '@/components/MetricsSection'
import { VantaBackground } from '@/components/VantaBackground'
import { VantaControls } from '@/components/VantaControls'
import { useVantaSettings } from '@/lib/vanta'
import { MetricsControls } from '@/components/MetricsControls'
import { MCPControls } from '@/components/MCPControls'
import { LanguageToggle } from '@/components/LanguageToggle'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useT, type TFunc } from '@/lib/i18n'
import { IssueCarousel } from '@/components/IssueCarousel'
import type { IssueItem, Pod } from '@/lib/api'

// Titles for the special (non-catalog) views; catalog resources and CRDs derive
// their own labels from the catalog / discovery.
function viewTitles(t: TFunc): Record<View, string> {
  return {
    overview: t('Cluster overview'),
    pods: 'Pods',
    topology: t('Cluster topology'),
    events: t('Cluster events'),
    helm: t('nav.helm'),
  }
}

const CTX_KEY = 'netsk8s.ctx'
const NS_KEY = 'netsk8s.ns'

export default function App() {
  // Design preview: open #loader to review the loading emblem in isolation.
  if (window.location.hash === '#loader') return <LoaderPreview />
  return <AppMain />
}

function AppMain() {
  // Persist the selected cluster/namespace so a refresh keeps you where you were.
  const [ctx, setCtxState] = useState<string | undefined>(() => localStorage.getItem(CTX_KEY) ?? undefined)
  const [ns, setNsState] = useState<string>(() => localStorage.getItem(NS_KEY) ?? '') // '' = all namespaces

  const setNs = (v: string) => {
    if (v) localStorage.setItem(NS_KEY, v)
    else localStorage.removeItem(NS_KEY)
    setNsState(v)
  }
  // Switching cluster resets the namespace (they differ per cluster).
  const setCtx = (v: string) => {
    localStorage.setItem(CTX_KEY, v)
    setCtxState(v)
    setNs('')
  }
  // View is deep-linked via the URL hash (#pods, #services, #crd:group/ver/res)
  // so views are bookmarkable and shareable.
  const [view, setViewState] = useState<string>(() => window.location.hash.slice(1) || 'overview')
  const setView = (v: string) => {
    window.location.hash = v
    setViewState(v)
    setSidebarOpen(false)
  }
  const [paletteOpen, setPaletteOpen] = useState(false)
  // Opened from the command palette's global resource search — independent of
  // whichever view is currently mounted, since a match can point to a kind
  // whose list view isn't open right now.
  const [searchTarget, setSearchTarget] = useState<DrawerTarget | null>(null)
  // Off-canvas below `lg`; the sidebar is always visible at `lg` and above.
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const vanta = useVantaSettings()
  const t = useT()

  // Global ⌘K / Ctrl+K opens the command palette.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen((o) => !o)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  const contextsQ = useQuery({ queryKey: ['contexts'], queryFn: api.contexts, refetchInterval: false })
  const healthQ = useQuery({ queryKey: ['health'], queryFn: api.health, staleTime: Infinity, refetchInterval: false })
  // The backend re-checks GitHub for a newer release once at startup and
  // every 24h on its own (see backend/internal/api/updatecheck.go); this
  // just polls that cached result often enough to notice within a session
  // that's been left open a while.
  const updateQ = useQuery({ queryKey: ['update-check'], queryFn: api.updateCheck, refetchInterval: 60 * 60 * 1000 })

  // Once contexts load, fall back to the kubeconfig's current context only if
  // there's no valid persisted selection (e.g. first visit or a stale one).
  useEffect(() => {
    if (!contextsQ.data?.length) return
    const valid = ctx && contextsQ.data.some((c) => c.name === ctx)
    if (!valid) {
      const fallback = contextsQ.data.find((c) => c.current)?.name ?? contextsQ.data[0].name
      localStorage.setItem(CTX_KEY, fallback)
      setCtxState(fallback)
    }
  }, [contextsQ.data, ctx])

  const overviewQ = useQuery({
    queryKey: ['overview', ctx],
    queryFn: () => api.overview(ctx!),
    enabled: !!ctx,
  })
  const nsQ = useQuery({
    queryKey: ['namespaces', ctx],
    queryFn: () => api.namespaces(ctx!),
    enabled: !!ctx,
    refetchInterval: false,
  })
  // Route CRDs (Gateway API / Traefik / Istio / …) served by this cluster.
  const routesQ = useQuery({
    queryKey: ['routekinds', ctx],
    queryFn: () => api.routeKinds(ctx!),
    enabled: !!ctx,
    staleTime: 5 * 60_000,
    refetchInterval: false,
  })
  const routes = routesQ.data ?? []
  // The route CRD selected by the current view, if any (#crd:group/ver/res).
  const activeRoute = view.startsWith('crd:') ? routes.find((r) => crdView(r) === view) : undefined

  // Every CRD the cluster serves (no allowlist) — the generic browser this
  // complements the curated "Network" route-CRD subset above.
  const crdKindsQ = useQuery({
    queryKey: ['crdkinds', ctx],
    queryFn: () => api.crdKinds(ctx!),
    enabled: !!ctx,
    staleTime: 5 * 60_000,
    refetchInterval: false,
  })
  const crdKinds = crdKindsQ.data ?? []
  const activeCRDKind = view.startsWith('crdkind:') ? crdKinds.find((k) => crdKindView(k) === view) : undefined

  const resDef = resourceByKey(view)
  const viewTitle = activeRoute?.label ?? activeCRDKind?.label ?? resDef?.label ?? viewTitles(t)[view as View] ?? t('Resource')

  return (
    <>
      <VantaBackground enabled={vanta.enabled} effect={vanta.effect} opacity={vanta.opacity} />
      <div className="relative z-10 flex h-screen">
        {/* Backdrop (mobile/tablet only, shown while the sidebar is open) */}
        {sidebarOpen && <div aria-hidden="true" className="fixed inset-0 z-30 bg-black/50 backdrop-blur-sm lg:hidden" onClick={() => setSidebarOpen(false)} />}

        {/* Sidebar: off-canvas below `lg`, static + always visible at `lg`+ */}
        <aside
          className={cn(
            'fixed inset-y-0 left-0 z-40 flex w-72 shrink-0 -translate-x-full flex-col gap-4 border-r bg-background/95 p-4 backdrop-blur-xl transition-transform duration-300 lg:static lg:translate-x-0 lg:bg-background/40',
            sidebarOpen && 'translate-x-0',
          )}
        >
          <div className="flex items-center gap-2.5 px-1 pt-1">
            <span className="shrink-0 drop-shadow-lg">
              <NavigatorLoader size={40} sky="green" />
            </span>
            <div>
              <h1 className="text-sm font-semibold leading-tight tracking-tight">
                Nets<span className="text-[color:var(--brand)]">k8</span> Navigator
              </h1>
              <p className="text-[11px] uppercase tracking-wider text-muted-foreground">{t('app.subtitle')}</p>
            </div>
          </div>

          <ContextSwitcher contexts={contextsQ.data ?? []} selected={ctx} onSelect={setCtx} />
          {ctx && !resDef?.clusterScoped && <NamespaceSelect namespaces={nsQ.data ?? []} selected={ns} onSelect={setNs} />}

          <div className="min-h-0 flex-1 overflow-y-auto pt-1">
            <ResourceNav active={view} onSelect={setView} routes={routes} crdKinds={crdKinds} />
          </div>

          <MetricsControls />
          <VantaControls {...vanta} />
          <MCPControls />
          <ThemeToggle />
          <LanguageToggle />
        </aside>

        {/* Main */}
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <header className="relative z-30 flex items-center justify-between gap-4 border-b bg-background/30 px-4 py-4 backdrop-blur-xl lg:px-6">
            <div className="flex min-w-0 items-center gap-3">
              <button
                onClick={() => setSidebarOpen(true)}
                className="-ml-1 shrink-0 rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground lg:hidden"
                aria-label={t('app.openMenu')}
              >
                <Menu className="size-5" />
              </button>
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Server className="size-3.5" />
                  <span className="truncate">{ctx ? shortContext(ctx) : '—'}</span>
                  <span className="text-border">/</span>
                  <span>{ns || t('app.allNamespaces')}</span>
                </div>
                <h2 className="mt-0.5 text-lg font-semibold tracking-tight">{viewTitle}</h2>
              </div>
            </div>
          </header>

          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4 lg:p-5">
            {contextsQ.isError && <ErrorBanner message={(contextsQ.error as Error).message} />}

            {!ctx && !contextsQ.isError && (
              <div className="flex h-[70vh] flex-col items-center justify-center gap-5 text-center">
                <NavigatorLoader
                  size={160}
                  sky="green"
                  state={contextsQ.isLoading ? 'connecting' : 'ready'}
                  label={contextsQ.isLoading ? t('app.connecting') : t('app.ready')}
                />
                <div>
                  <h2 className="text-xl font-semibold tracking-tight">
                    Nets<span className="text-[color:var(--brand)]">k8</span> Navigator
                  </h2>
                  <p className="mt-1 text-sm text-muted-foreground">{contextsQ.isLoading ? t('app.loadingClusters') : t('app.selectCluster')}</p>
                </div>
              </div>
            )}

            {ctx && view === 'overview' && (
              <OverviewPanel ctx={ctx} ns={ns} overview={overviewQ.data} loading={overviewQ.isLoading} error={overviewQ.error as Error | null} />
            )}
            {view === 'pods' && ctx && <LivePods ctx={ctx} ns={ns} />}
            {view === 'events' && ctx && <EventsPage ctx={ctx} ns={ns} />}
            {view === 'topology' && ctx && <TopologyView ctx={ctx} ns={ns} />}
            {view === 'helm' && ctx && <HelmView ctx={ctx} ns={ns} />}
            {ctx && resDef && <ResourceView key={resDef.key} def={resDef} ctx={ctx} ns={ns} />}
            {ctx && activeRoute && <CustomResourceView key={view} ctx={ctx} ns={ns} rk={activeRoute} />}
            {ctx && activeCRDKind && <CRDKindView key={view} ctx={ctx} ns={ns} rk={activeCRDKind} />}
          </div>
        </main>

        <CommandPalette
          open={paletteOpen}
          onOpenChange={setPaletteOpen}
          contexts={contextsQ.data ?? []}
          selectedCtx={ctx}
          onNavigate={setView}
          onSelectContext={setCtx}
          onOpenResource={setSearchTarget}
        />
        {ctx && <ResourceDrawer target={searchTarget} ctx={ctx} onClose={() => setSearchTarget(null)} />}
      </div>
      {healthQ.data?.demo && <FloatingBubble message={t('demo.banner')} href="https://github.com/robertobado/netsk8-navigator" />}
      {updateQ.data?.available && (
        <FloatingBubble
          key={updateQ.data.latest}
          message={`${t('update.available')}${updateQ.data.latest}`}
          href={updateQ.data.url ?? 'https://github.com/robertobado/netsk8-navigator/releases/latest'}
        />
      )}
    </>
  )
}

function LivePods({ ctx, ns }: { ctx: string; ns: string }) {
  const { pods, state } = useLivePods(ctx, ns)
  const [selected, setSelected] = useState<Pod | null>(null)
  const [target, setTarget] = useState<DrawerTarget | null>(null)
  return (
    <>
      <PodsTable ctx={ctx} ns={ns} pods={pods} connState={state} onSelect={setSelected} onOpenResource={setTarget} />
      <PodDrawer pod={selected} ctx={ctx} onClose={() => setSelected(null)} onOpenResource={setTarget} />
      <ResourceDrawer target={target} ctx={ctx} onClose={() => setTarget(null)} />
    </>
  )
}

function EventsPage({ ctx, ns }: Readonly<{ ctx: string; ns: string }>) {
  const [target, setTarget] = useState<DrawerTarget | null>(null)
  return (
    <>
      <EventsView ctx={ctx} ns={ns} onOpen={setTarget} />
      <ResourceDrawer target={target} ctx={ctx} onClose={() => setTarget(null)} />
    </>
  )
}

function issueToPod(it: IssueItem): Pod {
  return {
    name: it.name,
    namespace: it.namespace ?? '',
    status: it.reason || 'Pending',
    ready: 0,
    total: 0,
    restarts: 0,
    node: '',
    ip: '',
    age: it.since,
    containers: it.containers ?? [],
    ownerKind: '',
    ownerName: '',
    reason: it.reason,
    deletedAt: '',
    finalizers: [],
  }
}

function OverviewPanel({
  ctx,
  ns,
  overview,
  loading,
  error,
}: {
  ctx?: string
  ns: string
  overview?: import('@/lib/api').Overview
  loading: boolean
  error: Error | null
}) {
  const t = useT()
  const [pod, setPod] = useState<Pod | null>(null)
  const [nodeTarget, setNodeTarget] = useState<DrawerTarget | null>(null)
  const issuesQ = useQuery({ queryKey: ['issues', ctx], queryFn: () => api.issues(ctx!), enabled: !!ctx, refetchInterval: 10_000 })
  const issues = issuesQ.data

  const openItem = (it: IssueItem) => {
    if (it.kind === 'node') setNodeTarget({ kind: 'node', namespace: '', name: it.name, editable: false })
    else setPod(issueToPod(it))
  }

  return (
    <>
      {error && <ErrorBanner message={error.message} />}

      {/* 1) Metrics */}
      {ctx && <MetricsSection ctx={ctx} scope="cluster" onOpenNode={(name) => setNodeTarget({ kind: 'node', namespace: '', name, editable: false })} />}

      {/* 2) Panels */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <StatCard label="Nodes" value={ov(overview?.nodes)} sub={overview ? `${overview.readyNodes} ready` : undefined} icon={Server} loading={loading} />
        <StatCard label="Pods" value={ov(overview?.pods)} icon={Boxes} loading={loading} />
        <StatCard label="Namespaces" value={ov(overview?.namespaces)} icon={Layers} loading={loading} />
        <StatCard label="Running" value={ov(overview?.running)} tone="ok" icon={Activity} loading={loading} />
        <StatCard label="Pending" value={ov(overview?.pending)} tone="warn" icon={CircleDot} loading={loading} />
        <StatCard label="Failed" value={ov(overview?.failed)} tone="err" icon={CircleAlert} loading={loading} />
      </div>

      {/* 3) Carousels — one panel per issue category that has items. */}
      {!!(issues && issues.nodesNotReady.length + issues.pending.length + issues.failed.length > 0) && (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {issues.nodesNotReady.length > 0 && (
            <IssueCarousel title={t('Not-ready nodes')} icon={Server} items={issues.nodesNotReady} tone="err" onOpen={openItem} />
          )}
          {issues.pending.length > 0 && <IssueCarousel title="Pending" icon={CircleDot} items={issues.pending} tone="warn" onOpen={openItem} />}
          {issues.failed.length > 0 && <IssueCarousel title="Failed" icon={CircleAlert} items={issues.failed} tone="err" onOpen={openItem} />}
        </div>
      )}

      {ctx && <LivePods ctx={ctx} ns={ns} />}
      {ctx && <PodDrawer pod={pod} ctx={ctx} onClose={() => setPod(null)} onOpenResource={setNodeTarget} />}
      {ctx && <ResourceDrawer target={nodeTarget} ctx={ctx} onClose={() => setNodeTarget(null)} />}
    </>
  )
}

const ov = (v?: number) => (v === undefined ? '—' : v.toLocaleString('pt-BR'))

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-3 rounded-xl border border-[color:var(--err)]/30 bg-[color:var(--err)]/10 px-4 py-3 text-sm">
      <CircleAlert className="mt-0.5 size-4 shrink-0 text-[color:var(--err)]" />
      <div>
        <p className="font-medium text-[color:var(--err)]">Falha ao consultar o cluster</p>
        <p className="mt-0.5 break-all text-muted-foreground">{message}</p>
      </div>
    </div>
  )
}
