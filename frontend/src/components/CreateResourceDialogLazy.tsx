import { lazy, Suspense } from 'react'
import { Loader2 } from 'lucide-react'
import type { CreatedResource, ManifestKind } from '@/lib/api'
import { useT } from '@/lib/i18n'

// Same code-splitting rationale as ManifestPanelLazy: Monaco is heavy, so it
// only loads once the dialog is actually opened.
const CreateResourceDialog = lazy(() => import('./CreateResourceDialog').then((m) => ({ default: m.CreateResourceDialog })))

interface Props {
  ctx: string
  kind: ManifestKind
  namespace: string
  clusterScoped: boolean
  open: boolean
  onClose: () => void
  onCreated: (result: CreatedResource) => void
}

export function CreateResourceDialogLazy(props: Readonly<Props>) {
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
      <CreateResourceDialog {...props} />
    </Suspense>
  )
}
