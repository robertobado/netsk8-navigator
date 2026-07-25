import { Command } from 'cmdk'
import { Boxes, LayoutDashboard, Server, Share2, type LucideIcon } from 'lucide-react'
import type { ContextInfo } from '@/lib/api'
import { shortContext } from '@/lib/utils'
import { RESOURCES } from '@/lib/resources'
import { useT } from '@/lib/i18n'

// Navigable views: the special ones plus every catalogued resource.
function useViewItems(): { view: string; label: string; icon: LucideIcon }[] {
  const t = useT()
  return [
    { view: 'overview', label: t('nav.overview'), icon: LayoutDashboard },
    { view: 'pods', label: t('nav.pods'), icon: Boxes },
    ...RESOURCES.map((r) => ({ view: r.key, label: r.label, icon: r.icon })),
    { view: 'topology', label: t('nav.topology'), icon: Share2 },
  ]
}

export function CommandPalette({
  open,
  onOpenChange,
  contexts,
  onNavigate,
  onSelectContext,
}: Readonly<{
  open: boolean
  onOpenChange: (v: boolean) => void
  contexts: ContextInfo[]
  onNavigate: (v: string) => void
  onSelectContext: (name: string) => void
}>) {
  const t = useT()
  const viewItems = useViewItems()
  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Command palette"
      className="fixed left-1/2 top-[20%] z-[100] w-[calc(100%-1.5rem)] max-w-xl -translate-x-1/2 overflow-hidden rounded-2xl border bg-popover/95 shadow-2xl shadow-black/50 backdrop-blur-2xl"
      overlayClassName="fixed inset-0 z-[99] bg-black/50 backdrop-blur-sm"
    >
      <Command.Input
        placeholder={t('Type a command or search...')}
        className="w-full border-b bg-transparent px-4 py-3.5 text-sm outline-none placeholder:text-muted-foreground"
      />
      <Command.List className="max-h-80 overflow-y-auto p-2">
        <Command.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">{t('No results.')}</Command.Empty>

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
