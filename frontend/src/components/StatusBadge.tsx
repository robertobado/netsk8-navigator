import { cn } from '@/lib/utils'

// Maps a k8s phase/status string to a semantic color tone.
function tone(status: string): 'ok' | 'warn' | 'err' | 'muted' {
  const s = status.toLowerCase()
  if (['running', 'ready', 'active', 'succeeded', 'true', 'bound'].includes(s)) return 'ok'
  if (['pending', 'containercreating', 'terminating', 'podinitializing'].includes(s)) return 'warn'
  if (['failed', 'crashloopbackoff', 'error', 'evicted', 'notready', 'false'].includes(s)) return 'err'
  return 'muted'
}

const styles: Record<string, string> = {
  ok: 'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25',
  warn: 'bg-[color:var(--warn)]/12 text-[color:var(--warn)] ring-[color:var(--warn)]/25',
  err: 'bg-[color:var(--err)]/12 text-[color:var(--err)] ring-[color:var(--err)]/25',
  muted: 'bg-muted text-muted-foreground ring-border',
}

export function StatusBadge({ status }: Readonly<{ status: string }>) {
  const t = tone(status)
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', styles[t])}>
      <span
        className={cn('size-1.5 rounded-full', {
          'bg-[color:var(--ok)]': t === 'ok',
          'bg-[color:var(--warn)] animate-pulse': t === 'warn',
          'bg-[color:var(--err)]': t === 'err',
          'bg-muted-foreground': t === 'muted',
        })}
      />
      {status}
    </span>
  )
}
