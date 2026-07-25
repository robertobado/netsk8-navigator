import { Activity } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { REFRESH_OPTIONS, setMetricsRefresh, useMetricsRefresh } from '@/lib/metrics'

export function MetricsControls() {
  const { ms } = useMetricsRefresh()
  const t = useT()
  return (
    <div className="space-y-2 rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <span className="flex items-center gap-1.5 text-xs font-medium">
        <Activity className="size-3.5 text-[color:var(--brand)]" /> {t('controls.metrics')}
      </span>
      <div className="grid grid-cols-5 gap-1">
        {REFRESH_OPTIONS.map((o) => (
          <button
            key={o.ms}
            type="button"
            onClick={() => setMetricsRefresh(o.ms)}
            title={o.ms === 0 ? t('Hide metrics') : `${t('Refresh every')} ${o.label}`}
            className={cn(
              'rounded-md py-1 text-xs transition-colors',
              o.ms === ms
                ? 'bg-[color:var(--brand)]/15 text-[color:var(--brand)] ring-1 ring-inset ring-[color:var(--brand)]/40'
                : 'bg-muted/50 text-muted-foreground hover:text-foreground',
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}
