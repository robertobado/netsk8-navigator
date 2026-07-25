import { Sparkles } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { VANTA_EFFECTS, type VantaSettings } from '@/lib/vanta'

export function VantaControls({ enabled, effect, opacity, setEnabled, setEffect, setOpacity }: Readonly<VantaSettings>) {
  const t = useT()
  return (
    <div className="space-y-3 rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          <Sparkles className="size-3.5 text-[color:var(--brand)]" /> {t('controls.background')}
        </span>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label="Ativar fundo animado"
          onClick={() => setEnabled(!enabled)}
          className={cn('relative h-5 w-9 shrink-0 rounded-full transition-colors', enabled ? 'bg-[color:var(--brand)]' : 'bg-muted')}
        >
          <span className={cn('absolute top-0.5 size-4 rounded-full bg-white shadow transition-all', enabled ? 'left-[1.125rem]' : 'left-0.5')} />
        </button>
      </div>

      {enabled && (
        <>
          <div className="grid grid-cols-3 gap-1">
            {VANTA_EFFECTS.map((e) => (
              <button
                key={e.key}
                type="button"
                onClick={() => setEffect(e.key)}
                className={cn(
                  'rounded-md px-2 py-1 text-xs transition-colors',
                  e.key === effect
                    ? 'bg-[color:var(--brand)]/15 text-[color:var(--brand)] ring-1 ring-inset ring-[color:var(--brand)]/40'
                    : 'bg-muted/50 text-muted-foreground hover:text-foreground',
                )}
              >
                {e.label}
              </button>
            ))}
          </div>

          <div className="space-y-1">
            <div className="flex items-center justify-between text-[11px] text-muted-foreground">
              <span>Opacidade</span>
              <span className="font-mono tabular-nums">{Math.round(opacity * 100)}%</span>
            </div>
            <input
              type="range"
              min={0.05}
              max={1}
              step={0.05}
              value={opacity}
              onChange={(e) => setOpacity(Number(e.target.value))}
              aria-label="Opacidade do fundo animado"
              className="w-full accent-[color:var(--brand)]"
            />
          </div>
        </>
      )}
    </div>
  )
}
