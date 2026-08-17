import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, History, Loader2, Undo2, X } from 'lucide-react'
import { rolloutHistory, rolloutUndo } from '@/lib/api'
import { useT } from '@/lib/i18n'

// Modal listing a Deployment's revision history (via its owned ReplicaSets)
// with an "Undo" per non-current revision. Chrome mirrors CommandPalette's
// (backdrop + centered panel).
export function RolloutHistory({
  ctx,
  namespace,
  name,
  open,
  onClose,
}: Readonly<{ ctx: string; namespace: string; name: string; open: boolean; onClose: () => void }>) {
  const t = useT()
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['rollout-history', ctx, namespace, name],
    queryFn: () => rolloutHistory(ctx, 'deployment', namespace, name),
    enabled: open,
  })
  const [confirming, setConfirming] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const undo = async (toRevision: number) => {
    setBusy(true)
    setError('')
    try {
      await rolloutUndo(ctx, 'deployment', namespace, name, toRevision)
      await qc.invalidateQueries({ queryKey: ['rollout-history', ctx, namespace, name] })
      await qc.invalidateQueries({ queryKey: ['detail'] })
      await qc.invalidateQueries({ queryKey: ['resources'] })
      setConfirming(null)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (!open) return null

  return (
    <>
      <div aria-hidden="true" className="fixed inset-0 z-[100] bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed left-1/2 top-[15%] z-[101] w-[calc(100%-1.5rem)] max-w-lg -translate-x-1/2 overflow-hidden rounded-2xl border bg-popover/95 shadow-2xl shadow-black/50 backdrop-blur-2xl">
        <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <History className="size-4" /> {t('Rollout history')}
          </h2>
          <button type="button" onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
            <X className="size-4" />
          </button>
        </div>
        <div className="max-h-96 overflow-y-auto p-2">
          {q.isLoading && (
            <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
            </div>
          )}
          {q.data?.length === 0 && <p className="p-4 text-center text-sm text-muted-foreground">{t('No revision history yet.')}</p>}
          {q.data?.map((rev) => (
            <div key={rev.revision} className="flex flex-wrap items-center justify-between gap-3 rounded-lg px-3 py-2.5 hover:bg-accent/40">
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-sm font-medium">
                  {t('Revision')} {rev.revision}
                  {rev.current && (
                    <span className="rounded-full bg-primary/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-primary">
                      {t('Current')}
                    </span>
                  )}
                </div>
                <p className="truncate text-xs text-muted-foreground">{rev.images.join(', ')}</p>
              </div>
              {!rev.current &&
                (confirming === rev.revision ? (
                  <div className="flex shrink-0 items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => undo(rev.revision)}
                      disabled={busy}
                      className="inline-flex items-center gap-1 rounded-lg bg-[color:var(--warn)]/90 px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50"
                    >
                      {busy ? <Loader2 className="size-3.5 animate-spin" /> : t('Confirm')}
                    </button>
                    <button type="button" onClick={() => setConfirming(null)} className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
                      {t('Cancel')}
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirming(rev.revision)}
                    className="inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    <Undo2 className="size-3.5" /> {t('Undo')}
                  </button>
                ))}
            </div>
          ))}
          {error && (
            <p className="flex items-center gap-1.5 px-3 py-2 text-xs text-[color:var(--err)]">
              <AlertTriangle className="size-3.5" /> {error}
            </p>
          )}
        </div>
      </div>
    </>
  )
}
