import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Info, Inbox, Loader2 } from 'lucide-react'
import { api } from '@/lib/api'
import { age, cn } from '@/lib/utils'

// Events involving a resource, most recent first. Warnings stand out; repeats show ×N.
export function EventsPanel({ ctx, namespace, name, kind }: Readonly<{ ctx: string; namespace: string; name: string; kind?: string }>) {
  const q = useQuery({
    queryKey: ['events', ctx, namespace, name, kind],
    queryFn: () => api.events(ctx, namespace, name, kind),
    refetchInterval: 10_000,
  })
  const events = q.data ?? []

  if (q.isLoading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> Carregando eventos...
      </div>
    )
  }
  if (events.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
        <Inbox className="size-5" /> Nenhum evento recente para este pod.
      </div>
    )
  }

  return (
    <div className="h-full overflow-auto p-4">
      <ul className="space-y-2">
        {events.map((e, i) => {
          const warn = e.type === 'Warning'
          return (
            <li
              key={`${e.reason}-${e.last}-${i}`}
              className={cn('rounded-xl border p-3', warn ? 'border-[color:var(--warn)]/30 bg-[color:var(--warn)]/[0.06]' : 'bg-card/40')}
            >
              <div className="flex items-center gap-2">
                {warn ? <AlertTriangle className="size-3.5 shrink-0 text-[color:var(--warn)]" /> : <Info className="size-3.5 shrink-0 text-muted-foreground" />}
                <span className={cn('text-sm font-medium', warn && 'text-[color:var(--warn)]')}>{e.reason}</span>
                {e.count > 1 && (
                  <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-muted-foreground">×{e.count}</span>
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
    </div>
  )
}
