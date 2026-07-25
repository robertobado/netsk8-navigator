import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type Column,
  type ColumnDef,
  type ColumnFiltersState,
  type FilterFn,
  type RowData,
  type SortingState,
} from '@tanstack/react-table'
import { ArrowUpDown, Check, ChevronRight, Inbox, ListFilter, Loader2, Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

// Optional extra control rendered in a column header, beside the sort toggle.
declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    headerAddon?: ReactNode
  }
}

interface DataTableProps<T> {
  title: string
  data: T[]
  columns: ColumnDef<T, unknown>[]
  headerExtra?: ReactNode
  loading?: boolean
  emptyLabel?: string
  onRowClick?: (row: T) => void
  renderSubRow?: (row: T) => ReactNode
  storageKey?: string // persist the sort order across reloads
  facets?: string[] // column ids that get a multi-select dropdown filter
  // Interactive expansion: if it returns content for a row, that row shows a
  // chevron and clicking it toggles an inline expansion (instead of onRowClick).
  expandable?: (row: T) => ReactNode | null
  // Row virtualization: only rows near the viewport (plus a buffer) are mounted,
  // so large live lists stay cheap. Incompatible with expandable/subrows.
  virtualize?: boolean
}

// Below this many rows, virtualization isn't worth its trade-offs (browser
// find, windowing overhead), so it stays off even when opted in.
const VIRTUALIZE_MIN = 80

// Multi-select column filter: keep rows whose value is in the chosen set.
const multiSelectFilter: FilterFn<unknown> = (row, columnId, value) => {
  if (!Array.isArray(value) || value.length === 0) return true
  return value.includes(String(row.getValue(columnId)))
}

// A per-column dropdown listing that column's distinct values as checkboxes.
function FacetFilter<T>({ column }: Readonly<{ column: Column<T, unknown> }>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const selected = (column.getFilterValue() as string[] | undefined) ?? []
  // Cheap to recompute and only read while the dropdown is open, so no memo —
  // this also keeps the values fresh as the table's faceted data changes.
  const options = Array.from(column.getFacetedUniqueValues().keys())
    .map((v) => (v == null ? '' : String(v)))
    .filter((v) => v !== '')
    .sort((a, b) => a.localeCompare(b))
  const toggle = (v: string) => {
    const next = selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]
    column.setFilterValue(next.length ? next : undefined)
  }
  return (
    <div className="relative inline-flex">
      <button
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
        title={t('Filter column')}
        className={cn('rounded p-0.5 transition-colors hover:text-foreground', selected.length ? 'text-[color:var(--brand)]' : 'opacity-40 hover:opacity-100')}
      >
        <ListFilter className="size-3" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setOpen(false)} />
          <div className="absolute left-0 top-full z-30 mt-1.5 max-h-64 w-52 overflow-auto rounded-lg border bg-popover p-1 text-foreground shadow-lg">
            {options.length === 0 && <div className="px-2 py-1.5 text-xs text-muted-foreground">{t('No values')}</div>}
            {selected.length > 0 && (
              <button
                onClick={() => column.setFilterValue(undefined)}
                className="mb-1 w-full rounded px-2 py-1 text-left text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                {t('Clear filter')}
              </button>
            )}
            {options.map((v) => (
              <button key={v} onClick={() => toggle(v)} className="flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs hover:bg-accent">
                <span
                  className={cn(
                    'flex size-3.5 shrink-0 items-center justify-center rounded border',
                    selected.includes(v) ? 'border-[color:var(--brand)] bg-[color:var(--brand)] text-white' : 'border-border',
                  )}
                >
                  {selected.includes(v) && <Check className="size-2.5" />}
                </span>
                <span className="truncate font-normal normal-case">{v}</span>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

// Generic resource table: sortable columns, global filter, sticky header.
// Shared by every resource view (pods, deployments, services, ...).
export function DataTable<T>({
  title,
  data,
  columns,
  headerExtra,
  loading,
  emptyLabel,
  onRowClick,
  renderSubRow,
  storageKey,
  facets,
  expandable,
  virtualize,
}: DataTableProps<T>) {
  const t = useT()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const hasExpand = !!expandable
  const totalCols = columns.length + (hasExpand ? 1 : 0)
  const sortKey = storageKey ? `netsk8.sort.${storageKey}` : ''
  const [sorting, setSorting] = useState<SortingState>(() => {
    if (!sortKey) return []
    try {
      const raw = localStorage.getItem(sortKey)
      return raw ? (JSON.parse(raw) as SortingState) : []
    } catch {
      return []
    }
  })
  const [filter, setFilter] = useState('')
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])

  useEffect(() => {
    if (sortKey) localStorage.setItem(sortKey, JSON.stringify(sorting))
  }, [sortKey, sorting])
  const rows = useMemo(() => data, [data])

  const table = useReactTable({
    data: rows,
    columns,
    defaultColumn: { filterFn: multiSelectFilter as FilterFn<T> },
    state: { sorting, globalFilter: filter, columnFilters },
    onSortingChange: setSorting,
    onGlobalFilterChange: setFilter,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
  })

  // Row virtualization (opt-in): render only rows near the viewport + a buffer.
  // Kept off below a threshold so short lists stay native (browser find works,
  // no windowing overhead) — it only pays off once there are many rows.
  const modelRows = table.getRowModel().rows
  const doVirtualize = !!virtualize && modelRows.length > VIRTUALIZE_MIN
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: modelRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 41, // approx one-line row height
    overscan: 12, // buffer rows above/below the viewport
    enabled: doVirtualize,
  })
  const vItems = doVirtualize ? virtualizer.getVirtualItems() : []
  const padTop = vItems.length > 0 ? vItems[0].start : 0
  const padBottom = vItems.length > 0 ? virtualizer.getTotalSize() - vItems[vItems.length - 1].end : 0

  return (
    <div className="overflow-hidden rounded-2xl border bg-card/60 backdrop-blur-xl">
      <div className="flex flex-wrap items-center justify-between gap-3 gap-y-2 border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold">{title}</h2>
          <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground tabular-nums">{table.getFilteredRowModel().rows.length}</span>
          {headerExtra}
        </div>
        <div className="flex w-full items-center gap-2 rounded-lg border bg-background/50 px-2.5 sm:w-auto">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t('Filter...')}
            className="w-full bg-transparent py-1.5 text-sm outline-none placeholder:text-muted-foreground sm:w-52"
          />
        </div>
      </div>

      <div ref={scrollRef} className="max-h-[calc(100vh-16rem)] overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 z-10 bg-card/95 backdrop-blur-xl">
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b">
                {hasExpand && <th className="sticky left-0 z-20 w-6 bg-card" />}
                {hg.headers.map((h, i) => (
                  <th
                    key={h.id}
                    className={cn('px-4 py-2.5 text-left text-xs font-medium text-muted-foreground', !hasExpand && i === 0 && 'sticky left-0 z-20 bg-card')}
                  >
                    <div className="inline-flex items-center gap-1">
                      <button className="inline-flex items-center gap-1 transition-colors hover:text-foreground" onClick={h.column.getToggleSortingHandler()}>
                        {typeof h.column.columnDef.header === 'string' ? t(h.column.columnDef.header) : flexRender(h.column.columnDef.header, h.getContext())}
                        {h.column.getCanSort() && <ArrowUpDown className="size-3 opacity-40" />}
                      </button>
                      {facets?.includes(h.column.id) && <FacetFilter column={h.column} />}
                      {h.column.columnDef.meta?.headerAddon}
                    </div>
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {padTop > 0 && (
              <tr>
                <td colSpan={totalCols} style={{ height: padTop, padding: 0, border: 0 }} />
              </tr>
            )}
            {(doVirtualize ? vItems.map((vi) => modelRows[vi.index]) : modelRows).map((row) => {
              const sub = renderSubRow?.(row.original)
              const exp = expandable?.(row.original) ?? null
              const isOpen = expanded.has(row.id)
              const clickable = exp ? () => toggleExpanded(row.id) : onRowClick ? () => onRowClick(row.original) : undefined
              return (
                <Fragment key={row.id}>
                  <tr
                    onClick={clickable}
                    className={cn('transition-colors hover:bg-accent/40', sub ? '' : 'border-b border-border/50', clickable && 'cursor-pointer')}
                  >
                    {hasExpand && (
                      <td className="sticky left-0 z-[5] bg-card pl-3 align-top text-muted-foreground">
                        {exp && <ChevronRight className={cn('mt-2.5 size-3.5 transition-transform', isOpen && 'rotate-90')} />}
                      </td>
                    )}
                    {row.getVisibleCells().map((cell, i) => (
                      <td key={cell.id} className={cn('max-w-[24rem] truncate px-4 py-2.5 align-top', !hasExpand && i === 0 && 'sticky left-0 z-[5] bg-card')}>
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                  {sub && (
                    <tr className="border-b border-border/50">
                      <td colSpan={totalCols} className="px-4 pb-2.5 pt-0">
                        {sub}
                      </td>
                    </tr>
                  )}
                  {exp && isOpen && (
                    <tr className="border-b border-border/50 bg-background/30">
                      <td />
                      <td colSpan={totalCols - 1} className="px-4 pb-2.5 pt-0">
                        {exp}
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
            {padBottom > 0 && (
              <tr>
                <td colSpan={totalCols} style={{ height: padBottom, padding: 0, border: 0 }} />
              </tr>
            )}
            {modelRows.length === 0 && (
              <tr>
                <td colSpan={totalCols} className="px-4 py-16 text-center text-sm text-muted-foreground">
                  <span className="inline-flex items-center gap-2">
                    {loading ? (
                      <>
                        <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
                      </>
                    ) : (
                      <>
                        <Inbox className="size-4" /> {emptyLabel ?? t('No items to display.')}
                      </>
                    )}
                  </span>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
