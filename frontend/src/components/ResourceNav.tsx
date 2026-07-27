import { useEffect, useMemo, useState } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Bell, Boxes, ChevronRight, LayoutDashboard, Puzzle, Share2, Ship, Waypoints } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { CRDKind, RouteKind } from '@/lib/api'
import { RESOURCES, type ResourceDef } from '@/lib/resources'
import { crdKindView, crdView } from '@/lib/nav'
import { useT } from '@/lib/i18n'

interface NavItem {
  view: string
  label: string
  icon: LucideIcon
}

const asNav = (r: ResourceDef): NavItem => ({ view: r.key, label: r.label, icon: r.icon })
const inGroup = (g: ResourceDef['group']) => RESOURCES.filter((r) => r.group === g).map(asNav)

// Real clusters can easily serve dozens of CRDs (cert-manager, Prometheus
// Operator, Istio, Gatekeeper, ...) — a flat list gets unwieldy fast, so
// they're chunked by API group (apiVersion minus version) into a collapsible
// tree, the same idiom Lens uses for its Custom Resources section.
function groupByAPIGroup(crdKinds: CRDKind[]): { group: string; kinds: CRDKind[] }[] {
  const byGroup = new Map<string, CRDKind[]>()
  for (const k of crdKinds) {
    const bucket = byGroup.get(k.group)
    if (bucket) bucket.push(k)
    else byGroup.set(k.group, [k])
  }
  return [...byGroup.entries()].map(([group, kinds]) => ({ group, kinds })).sort((a, b) => a.group.localeCompare(b.group))
}

function CRDGroupNode({ group, kinds, active, onSelect }: Readonly<{ group: string; kinds: CRDKind[]; active: string; onSelect: (v: string) => void }>) {
  const containsActive = kinds.some((k) => crdKindView(k) === active)
  const [open, setOpen] = useState(containsActive)
  // Auto-expand to reveal the active selection (e.g. deep-linking via hash),
  // but never auto-collapse — that stays under the user's own control.
  useEffect(() => {
    if (containsActive) setOpen(true)
  }, [containsActive])

  return (
    <div className="flex flex-col gap-0.5">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-left text-xs font-medium text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
      >
        <ChevronRight className={cn('size-3.5 shrink-0 transition-transform', open && 'rotate-90')} />
        <span className="truncate">{group}</span>
        <span className="ml-auto shrink-0 tabular-nums text-muted-foreground/60">{kinds.length}</span>
      </button>
      {open && (
        <div className="ml-2.5 flex flex-col gap-0.5 border-l pl-2">
          {kinds.map((k) => {
            const view = crdKindView(k)
            const isActive = active === view
            return (
              <button
                key={view}
                onClick={() => onSelect(view)}
                className={cn(
                  'group relative flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-sm transition-colors',
                  isActive ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
                )}
              >
                {isActive && <span className="absolute left-0 top-1/2 h-4 w-0.5 -translate-y-1/2 rounded-full bg-primary" />}
                <Puzzle className={cn('size-3.5 shrink-0', isActive ? 'text-primary' : '')} />
                <span className="truncate">{k.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function ResourceNav({
  active,
  onSelect,
  routes = [],
  crdKinds = [],
}: Readonly<{ active: string; onSelect: (v: string) => void; routes?: RouteKind[]; crdKinds?: CRDKind[] }>) {
  const t = useT()
  // Nav is composed from the resource catalog + a few special views. Detected
  // route CRDs (HTTPRoute, IngressRoute, …) append under "Network", below Ingresses.
  const groups: { title?: string; items: NavItem[] }[] = [
    { items: [{ view: 'overview', label: t('nav.overview'), icon: LayoutDashboard }] },
    { title: t('group.workloads'), items: [{ view: 'pods', label: t('nav.pods'), icon: Boxes }, ...inGroup('Workloads')] },
    { title: t('group.rede'), items: [...inGroup('Network'), ...routes.map((rk) => ({ view: crdView(rk), label: rk.label, icon: Waypoints }))] },
    { title: t('group.config'), items: inGroup('Config') },
    { title: t('group.storage'), items: inGroup('Storage') },
    { title: t('group.rbac'), items: inGroup('RBAC') },
    { title: t('group.governanca'), items: inGroup('Governance') },
    {
      title: t('group.cluster'),
      items: [
        ...inGroup('Cluster'),
        { view: 'events', label: t('nav.events'), icon: Bell },
        { view: 'topology', label: t('nav.topology'), icon: Share2 },
        { view: 'helm', label: t('nav.helm'), icon: Ship },
      ],
    },
  ]
  // Every CRD the cluster serves, not just the curated "Network" subset above
  // — omitted entirely when the cluster has none, so it never shows as an
  // empty section. Chunked by API group (see CRDGroupNode) rather than a
  // flat list, since real clusters can serve dozens of CRDs.
  const crdGroups = useMemo(() => groupByAPIGroup(crdKinds), [crdKinds])

  return (
    <nav className="flex flex-col gap-4">
      {groups.map((group, gi) => (
        <div key={group.title ?? gi} className="flex flex-col gap-0.5">
          {group.title && <span className="px-2.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">{group.title}</span>}
          {group.items.map((item) => {
            const Icon = item.icon
            const isActive = active === item.view
            return (
              <button
                key={item.view}
                onClick={() => onSelect(item.view)}
                className={cn(
                  'group relative flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors',
                  isActive ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
                )}
              >
                {isActive && <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-primary" />}
                <Icon className={cn('size-4', isActive ? 'text-primary' : '')} />
                {item.label}
              </button>
            )
          })}
        </div>
      ))}
      {crdGroups.length > 0 && (
        <div className="flex flex-col gap-0.5">
          <span className="px-2.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">{t('group.customResources')}</span>
          {crdGroups.map(({ group, kinds }) => (
            <CRDGroupNode key={group} group={group} kinds={kinds} active={active} onSelect={onSelect} />
          ))}
        </div>
      )}
    </nav>
  )
}
