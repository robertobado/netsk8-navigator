import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createColumnHelper, type ColumnDef } from '@tanstack/react-table'
import { api, type CRDItem, type CRDKind } from '@/lib/api'
import { age } from '@/lib/utils'
import { CRDResourceDrawer } from './CRDResourceDrawer'
import { DataTable } from './DataTable'
import { tf, useT } from '@/lib/i18n'

const col = createColumnHelper<CRDItem>()

// Generic list for ANY CRD the cluster serves (no allowlist) — unlike
// CustomResourceView (the curated Gateway API/Traefik/Istio/Contour "Network"
// subset), an arbitrary CRD's schema is unknown at compile time, so the
// columns here are just Name / Namespace / Age; everything else lives in the
// shared CRDResourceDrawer's Details/YAML tabs.
export function CRDKindView({ ctx, ns, rk }: Readonly<{ ctx: string; ns: string; rk: CRDKind }>) {
  const t = useT()
  const [item, setItem] = useState<CRDItem | null>(null)
  const q = useQuery({
    queryKey: ['crdkind', ctx, rk.group, rk.version, rk.resource, ns],
    queryFn: () => api.crdList(ctx, rk, ns || undefined),
    enabled: !!ctx,
  })

  const columns = useMemo(
    () =>
      [
        col.accessor('name', { header: 'Name', cell: (c) => <span className="font-medium">{c.getValue()}</span> }),
        ...(rk.namespaced
          ? [col.accessor('namespace', { header: 'Namespace', cell: (c) => <span className="text-muted-foreground">{c.getValue() || '—'}</span> })]
          : []),
        col.accessor('age', {
          header: 'Age',
          cell: (c) => <span className="font-mono text-sm text-muted-foreground tabular-nums">{age(c.getValue())}</span>,
          sortingFn: (a, b) => new Date(a.original.age).getTime() - new Date(b.original.age).getTime(),
        }),
      ] as ColumnDef<CRDItem, unknown>[],
    [rk.namespaced],
  )

  return (
    <>
      <DataTable
        title={rk.label}
        data={q.data ?? []}
        columns={columns}
        loading={q.isLoading}
        storageKey={`crdkind-${rk.resource}`}
        facets={rk.namespaced ? ['namespace'] : []}
        emptyLabel={tf(t, 'No {kind} to display.', { kind: rk.kind })}
        onRowClick={setItem}
      />
      <CRDResourceDrawer ctx={ctx} rk={rk} item={item} onClose={() => setItem(null)} onDeleted={() => q.refetch()} />
    </>
  )
}
