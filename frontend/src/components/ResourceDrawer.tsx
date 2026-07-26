import { useEffect, useState } from 'react'
import { Bell, ChevronLeft, FileCode2, LayoutList, X } from 'lucide-react'
import { KINDS_WITH_DETAIL, SLUG_TO_KIND, type ManifestKind, type Pod } from '@/lib/api'
import { cn } from '@/lib/utils'
import { ManifestPanelLazy as ManifestPanel } from './ManifestPanelLazy'
import { DetailView } from './DetailView'
import { EventsPanel } from './EventsPanel'
import { PodDrawer } from './PodDrawer'
import { ResourceActions } from './ResourceActions'
import { useT } from '@/lib/i18n'

export interface DrawerTarget {
  kind: ManifestKind
  namespace: string
  name: string
  editable?: boolean // defaults to true; owner/node detail views pass false
}

type Tab = 'detail' | 'events' | 'yaml'

// Slide-over showing a resource: a structured detail view + the raw YAML.
export function ResourceDrawer({ target, ctx, onClose }: Readonly<{ target: DrawerTarget | null; ctx: string; onClose: () => void }>) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('detail')
  const [pod, setPod] = useState<Pod | null>(null) // drill-down into a workload's pod
  const [stack, setStack] = useState<DrawerTarget[]>([]) // in-drawer navigation (e.g. ingress → service)

  // The currently shown resource: the deepest drill-down, else the entry target.
  const cur = stack.length > 0 ? stack[stack.length - 1] : target
  const hasDetail = !!cur && KINDS_WITH_DETAIL.has(cur.kind)

  // Reset navigation whenever a new entry target opens.
  useEffect(() => {
    setStack([])
  }, [target])

  useEffect(() => {
    setTab(KINDS_WITH_DETAIL.has(cur?.kind as ManifestKind) ? 'detail' : 'yaml')
  }, [cur?.kind, cur?.name])

  useEffect(() => {
    if (!target) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (stack.length > 0) setStack((s) => s.slice(0, -1))
      else onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [target, onClose, stack.length])

  const open = !!target

  return (
    <>
      <div
        aria-hidden="true"
        className={cn('fixed inset-0 z-40 bg-black/50 backdrop-blur-sm transition-opacity', open ? 'opacity-100' : 'pointer-events-none opacity-0')}
        onClick={onClose}
      />
      <aside
        className={cn(
          'fixed right-0 top-0 z-50 flex h-full w-full max-w-3xl flex-col border-l bg-card shadow-2xl transition-transform duration-300',
          open ? 'translate-x-0' : 'translate-x-full',
        )}
      >
        {cur && (
          <>
            <header className="flex items-start justify-between gap-4 border-b px-5 py-4">
              <div className="flex min-w-0 items-start gap-2">
                {stack.length > 0 && (
                  <button
                    onClick={() => setStack((s) => s.slice(0, -1))}
                    title={t('Back')}
                    className="mt-0.5 rounded-lg p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    <ChevronLeft className="size-4" />
                  </button>
                )}
                <div className="min-w-0">
                  <span className="text-[10px] font-semibold uppercase tracking-wider text-primary">{cur.kind}</span>
                  <h2 className="truncate text-base font-semibold">{cur.name}</h2>
                  <p className="mt-0.5 text-xs text-muted-foreground">{cur.namespace || 'cluster-scoped'}</p>
                </div>
              </div>
              <button onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
                <X className="size-4" />
              </button>
            </header>

            <ResourceActions
              ctx={ctx}
              kind={cur.kind}
              namespace={cur.namespace}
              name={cur.name}
              editable={cur.editable ?? true}
              onDeleted={() => (stack.length > 0 ? setStack((s) => s.slice(0, -1)) : onClose())}
            />

            <div className="flex gap-1 overflow-x-auto border-b px-5 py-2.5">
              {hasDetail && <TabButton active={tab === 'detail'} onClick={() => setTab('detail')} icon={LayoutList} label={t('Details')} />}
              <TabButton active={tab === 'events'} onClick={() => setTab('events')} icon={Bell} label={t('nav.events')} />
              <TabButton active={tab === 'yaml'} onClick={() => setTab('yaml')} icon={FileCode2} label="YAML" />
            </div>

            <div className="min-h-0 flex-1">
              {hasDetail && tab === 'detail' && (
                <DetailView
                  key={`d-${cur.kind}-${cur.namespace}-${cur.name}`}
                  ctx={ctx}
                  kind={cur.kind}
                  namespace={cur.namespace}
                  name={cur.name}
                  onOpenPod={setPod}
                  onOpenResource={(target) => setStack((s) => [...s, { ...target, editable: false }])}
                />
              )}
              {tab === 'events' && (
                <EventsPanel key={`e-${cur.kind}-${cur.name}`} ctx={ctx} namespace={cur.namespace} name={cur.name} kind={SLUG_TO_KIND[cur.kind]} />
              )}
              {tab === 'yaml' && (
                <ManifestPanel
                  key={`y-${cur.kind}-${cur.namespace}-${cur.name}`}
                  ctx={ctx}
                  kind={cur.kind}
                  namespace={cur.namespace}
                  name={cur.name}
                  editable={cur.editable ?? true}
                />
              )}
            </div>
          </>
        )}
      </aside>

      {/* Drill-down: a workload's pod opens the full pod drawer on top. */}
      <PodDrawer pod={pod} ctx={ctx} onClose={() => setPod(null)} />
    </>
  )
}

function TabButton({ active, onClick, icon: Icon, label }: Readonly<{ active: boolean; onClick: () => void; icon: typeof FileCode2; label: string }>) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-4" />
      {label}
    </button>
  )
}
