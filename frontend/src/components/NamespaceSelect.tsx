import { useEffect, useRef, useState } from 'react'
import { Check, ChevronsUpDown, Layers, Search } from 'lucide-react'
import type { NamespaceInfo } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

interface Props {
  namespaces: NamespaceInfo[]
  selected: string // '' = all
  onSelect: (ns: string) => void
}

// Namespace filter for the sidebar (below the cluster switcher). '' means all.
export function NamespaceSelect({ namespaces, selected, onSelect }: Readonly<Props>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

  const filtered = namespaces.filter((n) => n.name.toLowerCase().includes(query.toLowerCase()))

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-3 rounded-xl border bg-card/70 px-3 py-2.5 text-left backdrop-blur-xl transition-colors hover:border-primary/40"
      >
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/15 text-primary">
          <Layers className="size-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold">{selected || t('ns.all')}</span>
          <span className="block truncate text-xs text-muted-foreground">{t('ns.label')}</span>
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
              placeholder={t('ns.search')}
              className="w-full bg-transparent py-2.5 text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
          <div className="max-h-72 overflow-y-auto p-1.5">
            <Item
              label={t('ns.all')}
              active={selected === ''}
              onClick={() => {
                onSelect('')
                setOpen(false)
              }}
            />
            {filtered.map((n) => (
              <Item
                key={n.name}
                label={n.name}
                active={selected === n.name}
                onClick={() => {
                  onSelect(n.name)
                  setOpen(false)
                }}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function Item({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn('flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-sm transition-colors hover:bg-accent', active && 'bg-accent')}
    >
      <Check className={cn('size-4 shrink-0 text-primary', active ? 'opacity-100' : 'opacity-0')} />
      <span className="truncate">{label}</span>
    </button>
  )
}
