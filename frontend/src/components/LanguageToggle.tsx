import { Languages } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAppPrefs } from '@/lib/preferences'
import { LANGUAGES, setLanguage, useT } from '@/lib/i18n'

// Compact language switch (part of app preferences). A precursor to the full
// preferences screen; kept in the sidebar so i18n is reachable and testable.
export function LanguageToggle() {
  const t = useT()
  const lang = useAppPrefs().language
  return (
    <div className="flex items-center justify-between rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <span className="flex items-center gap-1.5 text-xs font-medium">
        <Languages className="size-3.5 text-[color:var(--brand)]" /> {t('controls.language')}
      </span>
      <span className="inline-flex overflow-hidden rounded-md border text-[10px] font-medium">
        {LANGUAGES.map((l) => (
          <button
            key={l.code}
            type="button"
            onClick={() => setLanguage(l.code)}
            className={cn('px-2 py-0.5 transition-colors', l.code === lang ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground')}
          >
            {l.label}
          </button>
        ))}
      </span>
    </div>
  )
}
