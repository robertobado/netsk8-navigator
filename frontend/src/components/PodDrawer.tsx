import { useEffect, useState } from 'react'
import { Box, Bell, ChevronDown, FileCode2, LayoutList, Network, ScrollText, TerminalSquare, X } from 'lucide-react'
import type { Pod } from '@/lib/api'
import { cn } from '@/lib/utils'
import { StatusBadge } from './StatusBadge'
import { LogsPanel } from './LogsPanel'
import { TerminalPanel } from './TerminalPanel'
import { ManifestPanelLazy as ManifestPanel } from './ManifestPanelLazy'
import { DetailView } from './DetailView'
import { EventsPanel } from './EventsPanel'
import { ResourceActions } from './ResourceActions'
import { PortForwardPanel } from './PortForwardPanel'
import { useT } from '@/lib/i18n'
import type { DrawerTarget } from './ResourceDrawer'

type Tab = 'detail' | 'events' | 'logs' | 'terminal' | 'forward' | 'yaml'

export function PodDrawer({
  pod,
  ctx,
  onClose,
  onOpenResource,
}: {
  pod: Pod | null
  ctx: string
  onClose: () => void
  onOpenResource?: (t: DrawerTarget) => void
}) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('detail')
  const [container, setContainer] = useState<string | undefined>()

  useEffect(() => {
    setTab('detail')
    setContainer(pod?.containers?.[0])
  }, [pod])

  // Close on Escape.
  useEffect(() => {
    if (!pod) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [pod, onClose])

  const open = !!pod

  return (
    <>
      {/* Backdrop */}
      <div
        className={cn('fixed inset-0 z-40 bg-black/50 backdrop-blur-sm transition-opacity', open ? 'opacity-100' : 'pointer-events-none opacity-0')}
        onClick={onClose}
      />
      {/* Panel */}
      <aside
        className={cn(
          'fixed right-0 top-0 z-50 flex h-full w-full max-w-3xl flex-col border-l bg-card shadow-2xl transition-transform duration-300',
          open ? 'translate-x-0' : 'translate-x-full',
        )}
      >
        {pod && (
          <>
            <header className="flex items-start justify-between gap-4 border-b px-5 py-4">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <h2 className="truncate text-base font-semibold">{pod.name}</h2>
                  <StatusBadge status={pod.status} />
                </div>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {pod.namespace} · {pod.node || 'sem node'} · {pod.ip || 'sem IP'}
                </p>
              </div>
              <button onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
                <X className="size-4" />
              </button>
            </header>

            <ResourceActions ctx={ctx} kind="pod" namespace={pod.namespace} name={pod.name} editable={true} onDeleted={onClose} />

            <div className="flex items-center gap-3 border-b px-5 py-2.5">
              <div className="flex min-w-0 gap-1 overflow-x-auto">
                <TabButton active={tab === 'detail'} onClick={() => setTab('detail')} icon={LayoutList} label="Detalhes" />
                <TabButton active={tab === 'events'} onClick={() => setTab('events')} icon={Bell} label="Eventos" />
                <TabButton active={tab === 'logs'} onClick={() => setTab('logs')} icon={ScrollText} label="Logs" />
                <TabButton active={tab === 'terminal'} onClick={() => setTab('terminal')} icon={TerminalSquare} label="Terminal" />
                <TabButton active={tab === 'forward'} onClick={() => setTab('forward')} icon={Network} label={t('Forward')} />
                <TabButton active={tab === 'yaml'} onClick={() => setTab('yaml')} icon={FileCode2} label="YAML" />
              </div>
              {(tab === 'logs' || tab === 'terminal') && pod.containers.length > 0 && (
                <div className="ml-auto flex shrink-0 items-center gap-2">
                  <Box className="size-3.5 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Container</span>
                  <div className="relative">
                    <select
                      value={container}
                      onChange={(e) => setContainer(e.target.value)}
                      disabled={pod.containers.length < 2}
                      className="appearance-none rounded-lg border bg-background/50 py-1.5 pl-2.5 pr-7 text-sm outline-none transition-colors focus:border-primary/50 disabled:opacity-70"
                    >
                      {pod.containers.map((c) => (
                        <option key={c} value={c}>
                          {c}
                        </option>
                      ))}
                    </select>
                    {pod.containers.length > 1 && (
                      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    )}
                  </div>
                </div>
              )}
            </div>

            <div className="min-h-0 flex-1">
              {/* Keyed so switching pod/container/tab fully remounts the stream. */}
              {tab === 'detail' && (
                <DetailView key={`det-${pod.name}`} ctx={ctx} kind="pod" namespace={pod.namespace} name={pod.name} onOpenResource={onOpenResource} />
              )}
              {tab === 'events' && <EventsPanel key={`evt-${pod.name}`} ctx={ctx} namespace={pod.namespace} name={pod.name} kind="Pod" />}
              {tab === 'logs' && <LogsPanel key={`log-${pod.name}-${container}`} ctx={ctx} namespace={pod.namespace} pod={pod.name} container={container} />}
              {tab === 'terminal' && (
                <TerminalPanel key={`term-${pod.name}-${container}`} ctx={ctx} namespace={pod.namespace} pod={pod.name} container={container} />
              )}
              {tab === 'forward' && <PortForwardPanel key={`fwd-${pod.name}`} ctx={ctx} namespace={pod.namespace} name={pod.name} />}
              {tab === 'yaml' && <ManifestPanel key={`yaml-${pod.name}`} ctx={ctx} kind="pod" namespace={pod.namespace} name={pod.name} editable={false} />}
            </div>
          </>
        )}
      </aside>
    </>
  )
}

function TabButton({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: typeof ScrollText; label: string }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-4" />
      {label}
    </button>
  )
}
