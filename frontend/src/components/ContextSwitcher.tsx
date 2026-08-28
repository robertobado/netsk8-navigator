import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronsUpDown, Search, Server, Star } from 'lucide-react'
import type { ContextInfo } from '@/lib/api'
import { cn, shortContext } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { useAppPrefs, setAppPrefs } from '@/lib/preferences'

interface Props {
  contexts: ContextInfo[]
  selected?: string
  onSelect: (name: string) => void
}

export function ContextSwitcher({ contexts, selected, onSelect }: Readonly<Props>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  const { favorites } = useAppPrefs().contexts

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

  const toggleFavorite = (e: React.SyntheticEvent, name: string) => {
    e.stopPropagation()
    const next = favorites.includes(name) ? favorites.filter((n) => n !== name) : [...favorites, name]
    setAppPrefs({ contexts: { favorites: next } })
  }

  // Favorites float to the top (each group keeps its own alphabetical
  // order) — the direct payoff of starring a context in the kubeconfig
  // manager: it's faster to pick day-to-day, not just marked somewhere.
  const filtered = useMemo(() => {
    const q = query.toLowerCase()
    const matches = contexts.filter((c) => c.name.toLowerCase().includes(q))
    return [...matches].sort((a, b) => {
      const fa = favorites.includes(a.name),
        fb = favorites.includes(b.name)
      if (fa !== fb) return fa ? -1 : 1
      return a.name.localeCompare(b.name)
    })
  }, [contexts, query, favorites])

  const current = contexts.find((c) => c.name === selected)

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-3 rounded-xl border bg-card/70 px-3 py-2.5 text-left backdrop-blur-xl transition-colors hover:border-primary/40"
      >
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/15 text-primary">
          <Server className="size-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold">{current ? shortContext(current.name) : t('Select cluster')}</span>
          <span className="block truncate text-xs text-muted-foreground">{current ? current.server : `${contexts.length} ${t('contexts available')}`}</span>
        </span>
        <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" />
      </button>

      {open && (
        <div className="absolute z-50 mt-2 w-full overflow-hidden rounded-xl border bg-popover/95 shadow-2xl shadow-black/40 backdrop-blur-2xl">
          <div className="flex items-center gap-2 border-b px-3">
            <Search className="size-4 text-muted-foreground" />
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('Search cluster...')}
              className="w-full bg-transparent py-2.5 text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
          <div className="max-h-72 overflow-y-auto p-1.5">
            {filtered.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted-foreground">{t('No context found.')}</p>}
            {filtered.map((c) => (
              // A <button> can't nest another interactive element (the
              // favorite star), so the row is a plain flex container with
              // two sibling buttons instead of one button wrapping a span
              // pretending to be a second one.
              <div key={c.name} className={cn('flex items-center gap-1 rounded-lg', c.name === selected && 'bg-accent')}>
                <button
                  type="button"
                  onClick={() => {
                    onSelect(c.name)
                    setOpen(false)
                    setQuery('')
                  }}
                  className="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2.5 py-2 text-left text-sm transition-colors hover:bg-accent"
                >
                  <Check className={cn('size-4 shrink-0 text-primary', c.name === selected ? 'opacity-100' : 'opacity-0')} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{shortContext(c.name)}</span>
                    <span className="block truncate text-xs text-muted-foreground">{c.name}</span>
                  </span>
                  {c.name === selected && (
                    <span className="shrink-0 rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-medium text-primary">{t('current')}</span>
                  )}
                </button>
                <button
                  type="button"
                  onClick={(e) => toggleFavorite(e, c.name)}
                  aria-label={favorites.includes(c.name) ? t('kubeconfig.unfavorite', 'Remove favorite') : t('kubeconfig.favorite', 'Favorite')}
                  className={cn(
                    'shrink-0 rounded-lg p-2 transition-colors',
                    favorites.includes(c.name) ? 'text-[color:var(--warn)]' : 'text-muted-foreground/40 hover:text-muted-foreground',
                  )}
                >
                  <Star className="size-3.5" fill={favorites.includes(c.name) ? 'currentColor' : 'none'} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
