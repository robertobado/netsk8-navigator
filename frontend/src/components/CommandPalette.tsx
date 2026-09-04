import { useMemo, useState } from 'react'
import { Command } from 'cmdk'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { Boxes, LayoutDashboard, Search, Server, Share2, Ship, type LucideIcon } from 'lucide-react'
import { api, type ContextInfo, type ManifestKind } from '@/lib/api'
import { shortContext } from '@/lib/utils'
import { RESOURCES } from '@/lib/resources'
import { useT } from '@/lib/i18n'
import type { DrawerTarget } from './ResourceDrawer'

// Navigable views: the special ones plus every catalogued resource.
function useViewItems(): { view: string; label: string; icon: LucideIcon }[] {
  const t = useT()
  return [
    { view: 'overview', label: t('nav.overview'), icon: LayoutDashboard },
    { view: 'pods', label: t('nav.pods'), icon: Boxes },
    ...RESOURCES.map((r) => ({ view: r.key, label: r.label, icon: r.icon })),
    { view: 'topology', label: t('nav.topology'), icon: Share2 },
    { view: 'helm', label: t('nav.helm'), icon: Ship },
  ]
}

interface ResourceMatch {
  kind: ManifestKind
  namespace: string
  name: string
  resourceLabel: string
}

const MIN_SEARCH_LEN = 2
const MAX_MATCHES = 20

// addMatch dedupes by kind/namespace/name and appends to results.
function addMatch(results: ResourceMatch[], seen: Set<string>, kind: ManifestKind, namespace: string, name: string, resourceLabel: string) {
  const key = `${kind}/${namespace}/${name}`
  if (seen.has(key)) return
  seen.add(key)
  results.push({ kind, namespace, name, resourceLabel })
}

// matchingItems appends every item whose name contains needle, as kind, up to MAX_MATCHES.
function matchingItems(
  results: ResourceMatch[],
  seen: Set<string>,
  items: { name: string; namespace?: string }[],
  needle: string,
  kind: ManifestKind,
  label: string,
) {
  for (const item of items) {
    if (results.length >= MAX_MATCHES) return
    if (item.name?.toLowerCase().includes(needle)) addMatch(results, seen, kind, item.namespace ?? '', item.name, label)
  }
}

// matchesFromCachedResources scans every ['resources', ...] query react-query
// has already fetched this session for ctx, appending name matches.
function matchesFromCachedResources(results: ResourceMatch[], seen: Set<string>, qc: QueryClient, ctx: string, needle: string) {
  for (const entry of qc.getQueryCache().findAll({ queryKey: ['resources'] })) {
    if (results.length >= MAX_MATCHES) return
    const [, resource, entryCtx] = entry.queryKey as [string, string, string | undefined]
    if (entryCtx !== ctx) continue
    const def = RESOURCES.find((r) => r.resource === resource)
    if (!def) continue
    const items = (entry.state.data as { name: string; namespace?: string }[] | undefined) ?? []
    matchingItems(results, seen, items, needle, def.manifest, def.label)
  }
}

// Instance search across every resource kind: rather than a dedicated backend
// endpoint, this reuses whatever resource lists react-query has already
// fetched this session (any view the user visited) plus an on-demand,
// cluster-wide pods fetch (pods aren't otherwise cached — the Pods view
// streams them live over SSE instead of through react-query).
function useResourceMatches(ctx: string | undefined, search: string, enabled: boolean): ResourceMatch[] {
  const qc = useQueryClient()
  const podsQ = useQuery({
    queryKey: ['palette-pods', ctx],
    queryFn: () => api.pods(ctx!),
    enabled: enabled && !!ctx,
    staleTime: 30_000,
  })

  return useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!ctx || needle.length < MIN_SEARCH_LEN) return []
    const results: ResourceMatch[] = []
    const seen = new Set<string>()
    matchesFromCachedResources(results, seen, qc, ctx, needle)
    matchingItems(results, seen, podsQ.data ?? [], needle, 'pod', 'Pods')
    return results
  }, [qc, ctx, search, podsQ.data])
}

export function CommandPalette({
  open,
  onOpenChange,
  contexts,
  selectedCtx,
  onNavigate,
  onSelectContext,
  onOpenResource,
}: Readonly<{
  open: boolean
  onOpenChange: (v: boolean) => void
  contexts: ContextInfo[]
  selectedCtx?: string
  onNavigate: (v: string) => void
  onSelectContext: (name: string) => void
  onOpenResource: (target: DrawerTarget) => void
}>) {
  const t = useT()
  const viewItems = useViewItems()
  const [search, setSearch] = useState('')
  // Drop any typed search once the palette closes, so reopening starts
  // fresh. Adjusted during render (React's documented alternative to an
  // effect for "reset state when a prop changes") rather than in a
  // useEffect, so the reset is visible in the very render `open` flips in
  // instead of lagging a render behind.
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (!open) setSearch('')
  }
  const matches = useResourceMatches(selectedCtx, search, open)

  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Command palette"
      className="fixed left-1/2 top-[20%] z-[100] w-[calc(100%-1.5rem)] max-w-xl -translate-x-1/2 overflow-hidden rounded-2xl border bg-popover/95 shadow-2xl shadow-black/50 backdrop-blur-2xl"
      overlayClassName="fixed inset-0 z-[99] bg-black/50 backdrop-blur-sm"
    >
      <Command.Input
        value={search}
        onValueChange={setSearch}
        placeholder={t('Type a command or search...')}
        className="w-full border-b bg-transparent px-4 py-3.5 text-sm outline-none placeholder:text-muted-foreground"
      />
      <Command.List className="max-h-80 overflow-y-auto p-2">
        <Command.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">{t('No results.')}</Command.Empty>

        {matches.length > 0 && (
          <Command.Group
            heading={t('Resources')}
            className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted-foreground"
          >
            {matches.map((m) => (
              <Command.Item
                key={`${m.kind}/${m.namespace}/${m.name}`}
                value={`resource ${m.name} ${m.namespace} ${m.resourceLabel}`}
                onSelect={() => {
                  onOpenResource({ kind: m.kind, namespace: m.namespace, name: m.name })
                  onOpenChange(false)
                }}
                className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm aria-selected:bg-accent"
              >
                <Search className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{m.name}</span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {m.resourceLabel} {m.namespace && `· ${m.namespace}`}
                </span>
              </Command.Item>
            ))}
          </Command.Group>
        )}

        <Command.Group
          heading={t('Navigate')}
          className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted-foreground"
        >
          {viewItems.map((item) => {
            const Icon = item.icon
            return (
              <Command.Item
                key={item.view}
                value={`go ${item.label}`}
                onSelect={() => {
                  onNavigate(item.view)
                  onOpenChange(false)
                }}
                className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm aria-selected:bg-accent"
              >
                <Icon className="size-4 text-muted-foreground" />
                {item.label}
              </Command.Item>
            )
          })}
        </Command.Group>

        <Command.Group
          heading={t('Switch cluster')}
          className="mt-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted-foreground"
        >
          {contexts.map((c) => (
            <Command.Item
              key={c.name}
              value={`cluster ${c.name}`}
              onSelect={() => {
                onSelectContext(c.name)
                onOpenChange(false)
              }}
              className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm aria-selected:bg-accent"
            >
              <Server className="size-4 text-muted-foreground" />
              <span className="truncate">{shortContext(c.name)}</span>
            </Command.Item>
          ))}
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  )
}
