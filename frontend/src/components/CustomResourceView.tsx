import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createColumnHelper, type ColumnDef } from '@tanstack/react-table'
import { Bell, FileCode2, LayoutList, Loader2, X } from 'lucide-react'
import { api, crdDetail, crdManifest, type CRDItem, type RouteKind } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { DataTable } from './DataTable'
import { DetailBody } from './DetailView'
import { EventsPanel } from './EventsPanel'
import { tf, useT } from '@/lib/i18n'

const col = createColumnHelper<CRDItem>()

// Generic list for a route-like CRD (HTTPRoute, IngressRoute, VirtualService, …).
export function CustomResourceView({ ctx, ns, rk }: Readonly<{ ctx: string; ns: string; rk: RouteKind }>) {
  const t = useT()
  const [item, setItem] = useState<CRDItem | null>(null)
  const q = useQuery({
    queryKey: ['crd', ctx, rk.group, rk.version, rk.resource, ns],
    queryFn: () => api.crdList(ctx, rk, ns || undefined),
    enabled: !!ctx,
  })

  const columns = useMemo(
    () =>
      [
        col.accessor('name', { header: 'Name', cell: (c) => <span className="font-medium">{c.getValue()}</span> }),
        col.accessor('namespace', { header: 'Namespace', cell: (c) => <span className="text-muted-foreground">{c.getValue() || '—'}</span> }),
        col.accessor('hosts', { header: 'Hosts', cell: (c) => <span className="text-sm">{c.getValue() || '—'}</span> }),
        col.accessor('refs', { header: 'Gateways', cell: (c) => <span className="font-mono text-xs text-muted-foreground">{c.getValue() || '—'}</span> }),
        col.accessor('age', {
          header: 'Age',
          cell: (c) => <span className="font-mono text-sm text-muted-foreground tabular-nums">{age(c.getValue())}</span>,
          sortingFn: (a, b) => new Date(a.original.age).getTime() - new Date(b.original.age).getTime(),
        }),
      ] as ColumnDef<CRDItem, unknown>[],
    [],
  )

  return (
    <>
      <DataTable
        title={rk.label}
        data={q.data ?? []}
        columns={columns}
        loading={q.isLoading}
        storageKey={`crd-${rk.resource}`}
        facets={['namespace']}
        emptyLabel={tf(t, 'No {kind} to display.', { kind: rk.kind })}
        onRowClick={setItem}
      />
      <CRDDrawer ctx={ctx} rk={rk} item={item} onClose={() => setItem(null)} />
    </>
  )
}

type Tab = 'detail' | 'events' | 'yaml'

function CRDDrawer({ ctx, rk, item, onClose }: Readonly<{ ctx: string; rk: RouteKind; item: CRDItem | null; onClose: () => void }>) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('detail')
  useEffect(() => setTab('detail'), [item])
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
              <button onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
                <X className="size-4" />
              </button>
            </header>

            <div className="flex gap-1 border-b px-5 py-2.5">
              <TabButton active={tab === 'detail'} onClick={() => setTab('detail')} icon={LayoutList} label={t('Details')} />
              <TabButton active={tab === 'events'} onClick={() => setTab('events')} icon={Bell} label={t('nav.events')} />
              <TabButton active={tab === 'yaml'} onClick={() => setTab('yaml')} icon={FileCode2} label="YAML" />
            </div>

            <div className="min-h-0 flex-1">
              {tab === 'detail' && <CRDDetail key={`d-${item.name}`} ctx={ctx} rk={rk} namespace={item.namespace} name={item.name} />}
              {tab === 'events' && <EventsPanel key={`e-${item.name}`} ctx={ctx} namespace={item.namespace} name={item.name} kind={rk.kind} />}
              {tab === 'yaml' && <CRDYaml key={`y-${item.name}`} ctx={ctx} rk={rk} namespace={item.namespace} name={item.name} />}
            </div>
          </>
        )}
      </aside>
    </>
  )
}

function CRDDetail({ ctx, rk, namespace, name }: Readonly<{ ctx: string; rk: RouteKind; namespace: string; name: string }>) {
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

function CRDYaml({ ctx, rk, namespace, name }: Readonly<{ ctx: string; rk: RouteKind; namespace: string; name: string }>) {
  const t = useT()
  const q = useQuery({
    queryKey: ['crdmanifest', ctx, rk.group, rk.version, rk.resource, namespace, name],
    queryFn: () => crdManifest(ctx, rk, namespace, name),
  })
  if (q.isLoading)
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> {t('Loading YAML...')}
      </div>
    )
  if (q.isError) return <div className="p-6 text-center text-sm text-[color:var(--err)]">{(q.error as Error)?.message}</div>
  return (
    <div className="h-full overflow-auto p-4">
      <pre className="whitespace-pre font-mono text-xs leading-relaxed text-foreground/90">{q.data}</pre>
    </div>
  )
}

function TabButton({ active, onClick, icon: Icon, label }: Readonly<{ active: boolean; onClick: () => void; icon: typeof FileCode2; label: string }>) {
  return (
    <button
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
