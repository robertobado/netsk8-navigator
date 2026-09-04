import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bell, FileCode2, LayoutList, Loader2, X } from 'lucide-react'
import { crdDetail, type CRDRef } from '@/lib/api'
import { cn } from '@/lib/utils'
import { DetailBody } from './DetailView'
import { EventsPanel } from './EventsPanel'
import { ManifestPanelLazy as ManifestPanel } from './ManifestPanelLazy'
import { ResourceActions } from './ResourceActions'
import { useT } from '@/lib/i18n'

type Tab = 'detail' | 'events' | 'yaml'

// Detail/Events/YAML drawer for a single CRD instance — shared by the curated
// route-CRD browser (CustomResourceView) and the generic "any CRD" browser
// (CRDKindView). The YAML tab is a live ManifestPanel (edit + apply), and the
// action bar exposes Delete — both wired to the generic crd/{group}/{version}/
// {resource} endpoints via CRDRef, so this works for CRDs the app has never
// seen before, not just the small known set.
export function CRDResourceDrawer({
  ctx,
  rk,
  item,
  onClose,
  onDeleted,
}: Readonly<{
  ctx: string
  rk: CRDRef & { kind: string }
  item: { name: string; namespace: string } | null
  onClose: () => void
  onDeleted?: () => void
}>) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('detail')
  // Reset the tab whenever a new item opens. Adjusted during render rather
  // than in a useEffect (React's documented alternative for "reset state
  // when a prop changes"), so the reset is visible in the very render
  // `item` changes in.
  const [prevItem, setPrevItem] = useState(item)
  if (item !== prevItem) {
    setPrevItem(item)
    setTab('detail')
  }
  useEffect(() => {
    if (!item) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [item, onClose])
  const open = !!item

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
        {item && (
          <>
            <header className="flex items-start justify-between gap-4 border-b px-5 py-4">
              <div className="min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-primary">{rk.kind}</span>
                <h2 className="truncate text-base font-semibold">{item.name}</h2>
                <p className="mt-0.5 text-xs text-muted-foreground">{item.namespace || 'cluster-scoped'}</p>
              </div>
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="size-4" />
              </button>
            </header>

            <ResourceActions
              ctx={ctx}
              kind={rk}
              namespace={item.namespace}
              name={item.name}
              editable={true}
              onDeleted={() => {
                onClose()
                onDeleted?.()
              }}
            />

            <div className="flex gap-1 border-b px-5 py-2.5">
              <TabButton active={tab === 'detail'} onClick={() => setTab('detail')} icon={LayoutList} label={t('Details')} />
              <TabButton active={tab === 'events'} onClick={() => setTab('events')} icon={Bell} label={t('nav.events')} />
              <TabButton active={tab === 'yaml'} onClick={() => setTab('yaml')} icon={FileCode2} label="YAML" />
            </div>

            <div className="min-h-0 flex-1">
              {tab === 'detail' && <CRDDetail key={`d-${item.name}`} ctx={ctx} rk={rk} namespace={item.namespace} name={item.name} />}
              {tab === 'events' && <EventsPanel key={`e-${item.name}`} ctx={ctx} namespace={item.namespace} name={item.name} kind={rk.kind} />}
              {tab === 'yaml' && <ManifestPanel key={`y-${item.name}`} ctx={ctx} kind={rk} namespace={item.namespace} name={item.name} editable={true} />}
            </div>
          </>
        )}
      </aside>
    </>
  )
}

function CRDDetail({ ctx, rk, namespace, name }: Readonly<{ ctx: string; rk: CRDRef & { kind: string }; namespace: string; name: string }>) {
  const t = useT()
  const q = useQuery({
    queryKey: ['crddetail', ctx, rk.group, rk.version, rk.resource, namespace, name],
    queryFn: () => crdDetail(ctx, rk, namespace, name),
  })
  if (q.isLoading)
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> {t('Loading details...')}
      </div>
    )
  if (q.isError || !q.data) return <div className="p-6 text-center text-sm text-[color:var(--err)]">{(q.error as Error)?.message ?? t('Error')}</div>
  return <DetailBody d={q.data} ctx={ctx} kind={rk.kind} namespace={namespace} name={name} />
}

function TabButton({ active, onClick, icon: Icon, label }: Readonly<{ active: boolean; onClick: () => void; icon: typeof FileCode2; label: string }>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-4" />
      {label}
    </button>
  )
}
