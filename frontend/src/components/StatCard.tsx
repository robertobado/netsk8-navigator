import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface StatCardProps {
  label: string
  value: number | string
  sub?: string
  icon: LucideIcon
  tone?: 'primary' | 'ok' | 'warn' | 'err'
  loading?: boolean
  footer?: ReactNode
}

const toneRing: Record<string, string> = {
  primary: 'text-primary',
  ok: 'text-[color:var(--ok)]',
  warn: 'text-[color:var(--warn)]',
  err: 'text-[color:var(--err)]',
}

export function StatCard({ label, value, sub, icon: Icon, tone = 'primary', loading, footer }: Readonly<StatCardProps>) {
  return (
    <div className="group relative overflow-hidden rounded-xl border bg-card/60 px-4 py-2.5 backdrop-blur-xl transition-colors hover:border-primary/40">
      <div className="absolute -right-6 -top-6 size-24 rounded-full bg-primary/5 blur-2xl transition-opacity group-hover:opacity-100" />
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        <Icon className={cn('size-3.5', toneRing[tone])} />
      </div>
      <div className="mt-0.5 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tabular-nums tracking-tight">
          {loading ? <span className="inline-block h-7 w-14 animate-pulse rounded bg-muted" /> : value}
        </span>
        {sub && !loading && <span className="text-xs text-muted-foreground">{sub}</span>}
      </div>
      {footer}
    </div>
  )
}
