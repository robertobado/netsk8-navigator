import { useEffect } from 'react'
import { ExternalLink, Info, X } from 'lucide-react'
import { NavigatorLoader } from './Loader'
import { useT } from '@/lib/i18n'
import type { UpdateCheck } from '@/lib/api'

interface Props {
  open: boolean
  onClose: () => void
  version?: string
  update?: UpdateCheck
}

// Same dialog shape as PreferencesDialog (backdrop/centering-wrapper/panel,
// Escape-to-close, z-[90]) — opened from the version badge next to the
// sidebar logo, and (desktop app only) from the native "About" menu item via
// the show-about Wails event App.tsx listens for.
export function AboutDialog({ open, onClose, version, update }: Readonly<Props>) {
  const t = useT()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  let versionLabel = ''
  if (version) versionLabel = version === 'dev' ? t('about.devBuild') : `v${version}`

  return (
    <>
      <div aria-hidden="true" className="fixed inset-0 z-[90] bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="pointer-events-none fixed inset-0 z-[90] flex items-center justify-center p-4">
        <div className="pointer-events-auto flex w-full max-w-sm flex-col overflow-hidden rounded-2xl border bg-card shadow-2xl">
          <div className="flex items-center justify-between border-b px-5 py-3.5">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Info className="size-4" /> {t('about.title')}
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

          <div className="flex flex-col items-center gap-3 p-6 text-center">
            <NavigatorLoader size={64} sky="green" />
            <div>
              <h3 className="text-base font-semibold tracking-tight">
                Nets<span className="text-[color:var(--brand)]">k8</span> Navigator
              </h3>
              <p className="mt-0.5 font-mono text-xs text-muted-foreground">{versionLabel}</p>
            </div>
            <p className="text-xs text-muted-foreground">{t('about.tagline')}</p>

            {update?.available ? (
              <a
                href={update.url ?? 'https://github.com/robertobado/netsk8-navigator/releases/latest'}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-opacity hover:opacity-90"
              >
                {t('update.available')}
                {update.latest}
                <ExternalLink className="size-3" />
              </a>
            ) : (
              <p className="text-xs text-[color:var(--ok)]">{t('about.upToDate')}</p>
            )}

            <a
              href="https://github.com/robertobado/netsk8-navigator"
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground underline decoration-dotted underline-offset-2 transition-colors hover:text-foreground"
            >
              {t('about.viewOnGithub')}
              <ExternalLink className="size-3" />
            </a>
          </div>
        </div>
      </div>
    </>
  )
}
