import { useEffect } from 'react'
import { Settings2, X } from 'lucide-react'
import { VantaControls } from '@/components/VantaControls'
import { MCPControls } from '@/components/MCPControls'
import { ThemeToggle } from '@/components/ThemeToggle'
import { LanguageToggle } from '@/components/LanguageToggle'
import type { VantaSettings } from '@/lib/vanta'
import { useT } from '@/lib/i18n'

interface Props {
  open: boolean
  onClose: () => void
  vanta: VantaSettings
}

// Everything that used to live as a stack of cards at the bottom of the
// sidebar (animated background, MCP server, theme, language) now lives
// here instead — the sidebar itself keeps only MetricsControls, which is
// about the current view (refresh cadence), not app-wide preferences.
// Each moved-in control already renders as its own self-contained card
// (rounded-xl border bg-background/40 p-3), so they drop in unchanged.
export function PreferencesDialog({ open, onClose, vanta }: Readonly<Props>) {
  const t = useT()

  // Preferences apply live (every toggle here calls setAppPrefs immediately,
  // nothing is buffered/discardable) — Escape and a backdrop click are both
  // safe to close with, unlike the Helm/Create dialogs this borrows its
  // visual language from, which hold uncommitted form state.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <>
      {/* Backdrop is a separate, aria-hidden sibling (not a click handler on
          a wrapping div) — same pattern as RolloutHistory.tsx. The centering
          wrapper below is pointer-events-none so a click anywhere in its
          padding falls through to this backdrop and closes the dialog,
          without needing stopPropagation() on the panel itself. */}
      <div aria-hidden="true" className="fixed inset-0 z-[90] bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="pointer-events-none fixed inset-0 z-[90] flex items-center justify-center p-4">
        <div className="pointer-events-auto flex max-h-[85vh] w-full max-w-sm flex-col overflow-hidden rounded-2xl border bg-card shadow-2xl">
          <div className="flex items-center justify-between border-b px-5 py-3.5">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Settings2 className="size-4" /> {t('Preferences')}
            </h2>
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label={t('Close')}
            >
              <X className="size-4" />
            </button>
          </div>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
            <VantaControls {...vanta} />
            <MCPControls />
            <ThemeToggle />
            <LanguageToggle />
          </div>
        </div>
      </div>
    </>
  )
}
