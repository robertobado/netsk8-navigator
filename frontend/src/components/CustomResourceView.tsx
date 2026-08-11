import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import { api, type CRDItem, type RouteKind } from '@/lib/api'
import { age } from '@/lib/utils'
import { CRDResourceDrawer } from './CRDResourceDrawer'
import { DataTable } from './DataTable'
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
          sortFn: (a, b) => new Date(a.original.age).getTime() - new Date(b.original.age).getTime(),
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
      <CRDResourceDrawer
        ctx={ctx}
        rk={{ group: rk.group, version: rk.version, resource: rk.resource, kind: rk.kind }}
        item={item}
        onClose={() => setItem(null)}
        onDeleted={() => q.refetch()}
      />
    </>
  )
}
