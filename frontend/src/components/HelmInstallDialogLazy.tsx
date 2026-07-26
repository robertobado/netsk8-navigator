import { lazy, Suspense } from 'react'
import { Loader2 } from 'lucide-react'
import type { HelmRelease } from '@/lib/api'
import { useT } from '@/lib/i18n'

// Same rationale as ManifestPanelLazy/CreateResourceDialogLazy: Monaco only
// loads once this dialog actually opens.
const HelmInstallDialog = lazy(() => import('./HelmInstallDialog').then((m) => ({ default: m.HelmInstallDialog })))

interface Props {
  ctx: string
  mode: 'install' | 'upgrade'
  open: boolean
  namespace?: string
  existingRelease?: { namespace: string; name: string }
  initialValues?: string
  onClose: () => void
  onDone: (release: HelmRelease) => void
}

export function HelmInstallDialogLazy(props: Props) {
  const t = useT()
  if (!props.open) return null
  return (
    <Suspense
      fallback={
        <div className="fixed inset-0 z-[90] flex items-center justify-center gap-2 bg-black/50 text-sm text-white backdrop-blur-sm">
          <Loader2 className="size-4 animate-spin" /> {t('Loading editor…')}
        </div>
      }
    >
      <HelmInstallDialog {...props} />
    </Suspense>
  )
}
