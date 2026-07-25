import { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, Clock, Pin, type LucideIcon } from 'lucide-react'
import type { IssueItem } from '@/lib/api'
import { age, cn } from '@/lib/utils'

// Live-ticking relative age (updates every second).
function LiveAge({ since }: Readonly<{ since: string }>) {
  const [, tick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => tick((t) => t + 1), 1000)
    return () => clearInterval(id)
  }, [])
  return <>{age(since)}</>
}

const TONE: Record<string, string> = {
  warn: 'var(--warn)',
  err: 'var(--err)',
  muted: 'var(--muted-foreground)',
}

// Standalone panel that rotates through unhealthy items (pending/failed pods,
// not-ready nodes). Auto-advances unless pinned; click opens the item's details.
// Items arrive newest-first from the backend.
export function IssueCarousel({
  title,
  icon: Icon,
  items,
  tone = 'warn',
  onOpen,
}: Readonly<{ title: string; icon: LucideIcon; items: IssueItem[]; tone?: 'warn' | 'err' | 'muted'; onOpen: (item: IssueItem) => void }>) {
  const [i, setI] = useState(0)
  const [pinned, setPinned] = useState(false)
  const len = items.length

  useEffect(() => {
    if (pinned || len < 2) return
    const id = setInterval(() => setI((v) => v + 1), 4000)
    return () => clearInterval(id)
  }, [pinned, len])

  if (len === 0) return null
  const idx = ((i % len) + len) % len // wraps for both directions
  const cur = items[idx]
  const color = TONE[tone]
  const go = (delta: number) => setI((v) => v + delta)

  return (
    <div className="rounded-2xl border bg-card/60 p-4 backdrop-blur-xl">
      <div className="mb-2 flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-sm font-semibold">
          <Icon className="size-4" style={{ color }} /> {title}
        </span>
        <span className="rounded-full bg-muted px-2 py-0.5 text-xs tabular-nums text-muted-foreground">{len}</span>
      </div>
      <button
        type="button"
        onClick={() => onOpen(cur)}
        className="group block w-full rounded-lg px-1.5 py-1 text-left transition-colors hover:bg-accent/40"
        title="Abrir detalhes"
      >
        <div key={idx} className="nk-blur">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">{cur.name}</span>
              {cur.namespace && <span className="block truncate text-[11px] text-muted-foreground">{cur.namespace}</span>}
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground">
              <Clock className="size-3" />
              <LiveAge since={cur.since} />
            </span>
          </div>
          <div className="mt-1 flex items-center gap-2">
            <span
              className="shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-inset"
              style={{ color, background: `color-mix(in srgb, ${color} 12%, transparent)`, borderColor: `color-mix(in srgb, ${color} 30%, transparent)` }}
            >
              {cur.reason}
            </span>
            <span className="truncate text-[11px] text-muted-foreground" title={cur.message}>
              {cur.message || 'sem detalhe'}
            </span>
            <ChevronRight className="ml-auto size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-70" />
          </div>
        </div>
      </button>

      <div className="mt-1.5 flex items-center justify-between border-t border-border/50 pt-1.5">
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            onClick={() => go(-1)}
            disabled={len < 2}
            className="rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground disabled:opacity-30"
            aria-label="Anterior"
          >
            <ChevronLeft className="size-3.5" />
          </button>
          <span className="min-w-[3rem] text-center text-[10px] tabular-nums text-muted-foreground">
            {idx + 1} / {len}
          </span>
          <button
            type="button"
            onClick={() => go(1)}
            disabled={len < 2}
            className="rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground disabled:opacity-30"
            aria-label="Próximo"
          >
            <ChevronRight className="size-3.5" />
          </button>
        </div>
        <button
          type="button"
          onClick={() => setPinned((v) => !v)}
          title={pinned ? 'Retomar carrossel' : 'Fixar neste item'}
          className={cn(
            'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium transition-colors',
            pinned ? 'text-[color:var(--brand)]' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          <Pin className={cn('size-3 transition-transform', pinned ? 'rotate-0 fill-current' : 'rotate-45')} />
          {pinned ? 'fixado' : 'fixar'}
        </button>
      </div>
    </div>
  )
}
