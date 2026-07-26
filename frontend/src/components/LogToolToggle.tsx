import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

// Shared by LogsPanel and MultiPodLogsPanel's toolbars (timestamps/wrap/order/clear toggles).
export function LogToolToggle({ active, onClick, icon: Icon, title }: Readonly<{ active: boolean; onClick: () => void; icon: LucideIcon; title: string }>) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={cn(
        'rounded-md p-1.5 transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-3.5" />
    </button>
  )
}
