import { Monitor, Moon, Sun } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { THEME_MODE_OPTIONS, useAppPrefs, setAppPrefs, type ThemeMode } from '@/lib/preferences'
import { useT } from '@/lib/i18n'

const ICONS: Record<ThemeMode, LucideIcon> = { light: Sun, dark: Moon, auto: Monitor }

// Compact light/dark/auto switch (part of app preferences), styled to match
// LanguageToggle right below it in the sidebar.
export function ThemeToggle() {
  const t = useT()
  const theme = useAppPrefs().theme
  return (
    <div className="flex items-center justify-between rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <span className="flex items-center gap-1.5 text-xs font-medium">
        <Sun className="size-3.5 text-[color:var(--brand)]" /> {t('controls.theme')}
      </span>
      <span className="inline-flex overflow-hidden rounded-md border text-[10px] font-medium">
        {THEME_MODE_OPTIONS.map(({ mode, labelKey }) => {
          const Icon = ICONS[mode]
          return (
            <button
              key={mode}
              type="button"
              onClick={() => setAppPrefs({ theme: mode })}
              title={t(labelKey)}
              aria-label={t(labelKey)}
              aria-pressed={mode === theme}
              className={cn(
                'flex items-center gap-1 px-2 py-0.5 transition-colors',
                mode === theme ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              <Icon className="size-3" />
            </button>
          )
        })}
      </span>
    </div>
  )
}
