import { lazy, Suspense } from 'react'
import { Loader2 } from 'lucide-react'
import type { ManifestKind } from '@/lib/api'
import { useT } from '@/lib/i18n'

// Monaco is heavy (~3 MB), so the manifest editor is code-split and loaded on
// demand — only when a YAML tab is actually opened — keeping it out of the
// initial bundle. Consumers use this wrapper instead of ManifestPanel directly.
const ManifestPanel = lazy(() => import('./ManifestPanel').then((m) => ({ default: m.ManifestPanel })))

interface Props {
  ctx: string
  kind: ManifestKind
  namespace: string
  name: string
  editable: boolean
}

export function ManifestPanelLazy(props: Props) {
  const t = useT()
  return (
    <Suspense
      fallback={
        <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> {t('Loading editor…')}
        </div>
      }
    >
      <ManifestPanel {...props} />
    </Suspense>
  )
}
