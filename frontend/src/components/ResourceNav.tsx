import type { LucideIcon } from 'lucide-react'
import { Bell, Boxes, LayoutDashboard, Share2, Waypoints } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { RouteKind } from '@/lib/api'
import { RESOURCES, type ResourceDef } from '@/lib/resources'
import { crdView } from '@/lib/nav'
import { useT } from '@/lib/i18n'

interface NavItem {
  view: string
  label: string
  icon: LucideIcon
}

const asNav = (r: ResourceDef): NavItem => ({ view: r.key, label: r.label, icon: r.icon })
const inGroup = (g: ResourceDef['group']) => RESOURCES.filter((r) => r.group === g).map(asNav)

export function ResourceNav({ active, onSelect, routes = [] }: Readonly<{ active: string; onSelect: (v: string) => void; routes?: RouteKind[] }>) {
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
      items: [...inGroup('Cluster'), { view: 'events', label: t('nav.events'), icon: Bell }, { view: 'topology', label: t('nav.topology'), icon: Share2 }],
    },
  ]
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
    </nav>
  )
}
