import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Info, Inbox, Loader2, Radio, RefreshCw, Search, ServerCrash } from 'lucide-react'
import { api, kindToSlug } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import type { DrawerTarget } from './ResourceDrawer'

type Filter = 'all' | 'warning'

// Clusters can hold thousands of live events; render only the most recent slice
// (they're already sorted newest-first) and tell the user when we've capped, so
// a heavy list never silently degrades the view. Narrowing with the filter/search
// brings older matches back into range.
const MAX_SHOWN = 400

// Cluster-wide events, most recent first — a global feed with a type filter and
// free-text search. Each row links back to the involved object's drawer when we
// know how to open that kind.
export function EventsView({ ctx, ns, onOpen }: Readonly<{ ctx: string; ns: string; onOpen: (t: DrawerTarget) => void }>) {
  const q = useQuery({
    queryKey: ['allEvents', ctx, ns],
    queryFn: () => api.allEvents(ctx, ns),
    refetchInterval: 10_000,
  })
  const [filter, setFilter] = useState<Filter>('all')
  const [term, setTerm] = useState('')

  // Memoize on q.data so the derived useMemos below have a stable dependency
  // (a fresh `?? []` array each render would re-run them every time).
  const events = useMemo(() => q.data ?? [], [q.data])
  const warnings = useMemo(() => events.filter((e) => e.type === 'Warning').length, [events])

  const shown = useMemo(() => {
    const t = term.trim().toLowerCase()
    return events.filter((e) => {
      if (filter === 'warning' && e.type !== 'Warning') return false
      if (!t) return true
      return (
        e.reason.toLowerCase().includes(t) ||
        e.message.toLowerCase().includes(t) ||
        (e.objectName ?? '').toLowerCase().includes(t) ||
        (e.objectKind ?? '').toLowerCase().includes(t)
      )
    })
  }, [events, filter, term])

  const capped = shown.length > MAX_SHOWN
  const visible = capped ? shown.slice(0, MAX_SHOWN) : shown

  if (q.isLoading) {
    return (
      <div className="flex h-64 items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> Carregando eventos...
      </div>
    )
  }
  if (q.isError) {
    const raw = (q.error as Error).message
    const auth = /credential|exec:|Unauthorized|forbidden|token/i.test(raw)
    return (
      <div className="flex h-56 flex-col items-center justify-center gap-2 text-center">
        <ServerCrash className="size-6 text-[color:var(--err)]" />
        <p className="text-sm font-medium text-[color:var(--err)]">Não foi possível carregar os eventos deste cluster.</p>
        <p className="max-w-md text-xs text-muted-foreground">
          {auth
            ? 'A conexão com a API do Kubernetes falhou — credencial expirada ou sem permissão para listar eventos. Renove o login do cluster (ex.: credenciais AWS) e tente de novo.'
            : 'A API do Kubernetes não respondeu à listagem de eventos.'}
        </p>
        <p className="max-w-lg truncate font-mono text-[10px] text-muted-foreground/60" title={raw}>
          {raw}
        </p>
        <button
          onClick={() => q.refetch()}
          disabled={q.isFetching}
          className="mt-1 inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors hover:bg-accent disabled:opacity-50"
        >
          <RefreshCw className={cn('size-3.5', q.isFetching && 'animate-spin')} /> Tentar novamente
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-lg border bg-card/40 p-0.5 text-sm">
          <FilterTab active={filter === 'all'} onClick={() => setFilter('all')}>
            Todos <span className="tabular-nums text-muted-foreground">{events.length}</span>
          </FilterTab>
          <FilterTab active={filter === 'warning'} onClick={() => setFilter('warning')}>
            <AlertTriangle className="size-3.5 text-[color:var(--warn)]" /> Warnings <span className="tabular-nums text-muted-foreground">{warnings}</span>
          </FilterTab>
        </div>
        <span className="inline-flex items-center gap-1.5 rounded-full bg-[color:var(--ok)]/12 px-2 py-0.5 text-xs font-medium text-[color:var(--ok)] ring-1 ring-inset ring-[color:var(--ok)]/25">
          <Radio className="size-3 animate-pulse" /> ao vivo
        </span>
        <div className="relative ml-auto min-w-56 flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <input
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            placeholder="Filtrar por motivo, objeto ou mensagem..."
            className="w-full rounded-lg border bg-card/40 py-1.5 pl-8 pr-3 text-sm outline-none transition-colors focus:border-[color:var(--brand)]"
          />
        </div>
      </div>

      {shown.length === 0 ? (
        <div className="flex h-48 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
          <Inbox className="size-5" /> Nenhum evento {filter === 'warning' ? 'de warning ' : ''}encontrado.
        </div>
      ) : (
        <ul className="space-y-2">
          {visible.map((e, i) => {
            const warn = e.type === 'Warning'
            const slug = e.objectKind ? kindToSlug(e.objectKind) : null
            const clickable = !!slug && !!e.objectName
            return (
              <li
                key={`${e.objectKind}-${e.objectName}-${e.reason}-${e.last}-${i}`}
                className={cn('rounded-xl border p-3', warn ? 'border-[color:var(--warn)]/30 bg-[color:var(--warn)]/[0.06]' : 'bg-card/40')}
              >
                <div className="flex items-center gap-2">
                  {warn ? (
                    <AlertTriangle className="size-3.5 shrink-0 text-[color:var(--warn)]" />
                  ) : (
                    <Info className="size-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className={cn('text-sm font-medium', warn && 'text-[color:var(--warn)]')}>{e.reason}</span>
                  {e.count > 1 && (
                    <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-muted-foreground">×{e.count}</span>
                  )}
                  {e.objectKind && (
                    <span className="ml-1 inline-flex min-w-0 items-center gap-1.5 text-xs">
                      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{e.objectKind}</span>
                      {clickable ? (
                        <button
                          onClick={() => onOpen({ kind: slug!, namespace: e.objectNamespace ?? '', name: e.objectName! })}
                          className="min-w-0 truncate font-medium text-[color:var(--brand)] hover:underline"
                        >
                          {e.objectNamespace ? `${e.objectNamespace}/` : ''}
                          {e.objectName}
                        </button>
                      ) : (
                        <span className="min-w-0 truncate text-muted-foreground">
                          {e.objectNamespace ? `${e.objectNamespace}/` : ''}
                          {e.objectName}
                        </span>
                      )}
                    </span>
                  )}
                  <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums" title={e.last}>
                    há {age(e.last)}
                  </span>
                </div>
                <p className="mt-1 break-words text-xs text-muted-foreground">{e.message}</p>
                {e.source && <p className="mt-1 font-mono text-[10px] text-muted-foreground/70">{e.source}</p>}
              </li>
            )
          })}
        </ul>
      )}

      {capped && (
        <p className="pb-2 text-center text-xs text-muted-foreground">
          Mostrando os {MAX_SHOWN} eventos mais recentes de {shown.length}. Refine com o filtro ou a busca para ver os demais.
        </p>
      )}
    </div>
  )
}

function FilterTab({ active, onClick, children }: Readonly<{ active: boolean; onClick: () => void; children: React.ReactNode }>) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-3 py-1 font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}
