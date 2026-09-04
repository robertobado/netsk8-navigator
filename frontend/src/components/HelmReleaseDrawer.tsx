import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Check, FileCode2, History, Loader2, NotebookText, RotateCcw, Settings2, Trash2, X } from 'lucide-react'
import { helmReleaseHistory, helmReleaseManifest, helmReleaseRollback, helmReleaseStatus, helmReleaseUninstall, type HelmRelease } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { HelmStatusBadge } from './HelmView'
import { HelmInstallDialogLazy } from './HelmInstallDialogLazy'

type Tab = 'values' | 'manifest' | 'history' | 'notes'

export function HelmReleaseDrawer({
  ctx,
  target,
  onClose,
  onChanged,
}: Readonly<{ ctx: string; target: { namespace: string; name: string } | null; onClose: () => void; onChanged: () => void }>) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('values')
  const [confirmUninstall, setConfirmUninstall] = useState(false)
  const [uninstalling, setUninstalling] = useState(false)
  const [upgrading, setUpgrading] = useState(false)
  const [error, setError] = useState('')

  // Reset the drawer's local state whenever a new release opens. Adjusted
  // during render rather than in a useEffect (React's documented
  // alternative for "reset state when a prop changes"), so the reset is
  // visible in the very render `target` changes in.
  const [prevTarget, setPrevTarget] = useState(target)
  if (target !== prevTarget) {
    setPrevTarget(target)
    setTab('values')
    setConfirmUninstall(false)
    setError('')
  }

  const open = !!target
  const statusQ = useQuery({
    queryKey: ['helmReleaseStatus', ctx, target?.namespace, target?.name],
    queryFn: () => helmReleaseStatus(ctx, target!.namespace, target!.name),
    enabled: open,
  })
  const manifestQ = useQuery({
    queryKey: ['helmReleaseManifest', ctx, target?.namespace, target?.name],
    queryFn: () => helmReleaseManifest(ctx, target!.namespace, target!.name),
    enabled: open && tab === 'manifest',
  })
  const historyQ = useQuery({
    queryKey: ['helmReleaseHistory', ctx, target?.namespace, target?.name],
    queryFn: () => helmReleaseHistory(ctx, target!.namespace, target!.name),
    enabled: open && tab === 'history',
  })

  const uninstall = async () => {
    setUninstalling(true)
    setError('')
    try {
      await helmReleaseUninstall(ctx, target!.namespace, target!.name)
      onChanged()
      onClose()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setUninstalling(false)
    }
  }

  const rollback = async (revision: number) => {
    setError('')
    try {
      await helmReleaseRollback(ctx, target!.namespace, target!.name, revision)
      onChanged()
      statusQ.refetch()
      historyQ.refetch()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <>
      <div
        aria-hidden="true"
        className={cn('fixed inset-0 z-40 bg-black/50 backdrop-blur-sm transition-opacity', open ? 'opacity-100' : 'pointer-events-none opacity-0')}
        onClick={onClose}
      />
      <aside
        className={cn(
          'fixed right-0 top-0 z-50 flex h-full w-full max-w-3xl flex-col border-l bg-card shadow-2xl transition-transform duration-300',
          open ? 'translate-x-0' : 'translate-x-full',
        )}
      >
        {target && (
          <>
            <header className="flex items-start justify-between gap-4 border-b px-5 py-4">
              <div className="min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-primary">Helm release</span>
                <h2 className="truncate text-base font-semibold">{target.name}</h2>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {target.namespace} {statusQ.data && <HelmStatusBadge status={statusQ.data.status} />}
                </p>
              </div>
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="size-4" />
              </button>
            </header>

            <div className="flex flex-wrap items-center gap-3 border-b px-5 py-2.5">
              <button
                type="button"
                onClick={() => setUpgrading(true)}
                className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <Settings2 className="size-4" /> {t('Upgrade')}
              </button>
              {confirmUninstall ? (
                <div className="ml-auto flex items-center gap-2 rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-3 py-1.5">
                  <AlertTriangle className="size-4 shrink-0 text-[color:var(--err)]" />
                  <span className="text-xs text-[color:var(--err)]">{t('Uninstall this release?')}</span>
                  <button
                    type="button"
                    onClick={uninstall}
                    disabled={uninstalling}
                    className="inline-flex items-center gap-1 rounded-md bg-[color:var(--err)]/90 px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50"
                  >
                    {uninstalling ? <Loader2 className="size-3.5 animate-spin" /> : t('Confirm')}
                  </button>
                  <button type="button" onClick={() => setConfirmUninstall(false)} className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
                    {t('Cancel')}
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={() => setConfirmUninstall(true)}
                  className="ml-auto inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-[color:var(--err)] transition-colors hover:bg-[color:var(--err)]/10"
                >
                  <Trash2 className="size-4" /> {t('Uninstall')}
                </button>
              )}
            </div>
            {error && <p className="border-b px-5 py-2 text-xs text-[color:var(--err)]">{error}</p>}

            <div className="flex gap-1 overflow-x-auto border-b px-5 py-2.5">
              <TabButton active={tab === 'values'} onClick={() => setTab('values')} icon={FileCode2} label={t('Values')} />
              <TabButton active={tab === 'manifest'} onClick={() => setTab('manifest')} icon={FileCode2} label="Manifest" />
              <TabButton active={tab === 'history'} onClick={() => setTab('history')} icon={History} label={t('History')} />
              <TabButton active={tab === 'notes'} onClick={() => setTab('notes')} icon={NotebookText} label={t('Notes')} />
            </div>

            <div className="min-h-0 flex-1 overflow-auto p-4">
              {tab === 'values' && <YamlBlock loading={statusQ.isLoading} text={statusQ.data?.values} empty={t('This release has no custom values.')} />}
              {tab === 'manifest' && <YamlBlock loading={manifestQ.isLoading} text={manifestQ.data} empty="" />}
              {tab === 'history' && <HistoryList loading={historyQ.isLoading} revisions={historyQ.data ?? []} onRollback={rollback} />}
              {tab === 'notes' && <YamlBlock loading={statusQ.isLoading} text={statusQ.data?.notes} empty={t('This release has no notes.')} />}
            </div>
          </>
        )}
      </aside>

      {target && (
        <HelmInstallDialogLazy
          ctx={ctx}
          mode="upgrade"
          open={upgrading}
          existingRelease={{ namespace: target.namespace, name: target.name }}
          initialValues={statusQ.data?.values ?? ''}
          onClose={() => setUpgrading(false)}
          onDone={() => {
            setUpgrading(false)
            onChanged()
            statusQ.refetch()
          }}
        />
      )}
    </>
  )
}

function TabButton({ active, onClick, icon: Icon, label }: Readonly<{ active: boolean; onClick: () => void; icon: typeof FileCode2; label: string }>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-4" />
      {label}
    </button>
  )
}

function YamlBlock({ loading, text, empty }: Readonly<{ loading: boolean; text?: string; empty: string }>) {
  const t = useT()
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
      </div>
    )
  }
  if (!text?.trim()) {
    return <p className="text-sm text-muted-foreground">{empty}</p>
  }
  return (
    <pre className="h-full overflow-auto whitespace-pre-wrap break-words rounded-lg bg-background/50 p-3 font-mono text-xs leading-relaxed text-foreground/90">
      {text}
    </pre>
  )
}

function HistoryList({ loading, revisions, onRollback }: Readonly<{ loading: boolean; revisions: HelmRelease[]; onRollback: (revision: number) => void }>) {
  const t = useT()
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
      </div>
    )
  }
  if (revisions.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('No revision history yet.')}</p>
  }
  const current = Math.max(...revisions.map((r) => r.revision))
  return (
    <div className="space-y-1.5">
      {revisions.map((rev) => (
        <div key={rev.revision} className="flex items-center gap-3 rounded-lg border bg-background/40 px-3 py-2">
          <span className="w-10 shrink-0 font-mono text-sm tabular-nums text-muted-foreground">r{rev.revision}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs">{rev.chart}</span>
          <HelmStatusBadge status={rev.status} />
          <span className="shrink-0 text-xs text-muted-foreground">{age(rev.updated)}</span>
          {rev.revision === current ? (
            <span className="flex shrink-0 items-center gap-1 text-xs text-[color:var(--ok)]">
              <Check className="size-3.5" /> {t('current')}
            </span>
          ) : (
            <button
              type="button"
              onClick={() => onRollback(rev.revision)}
              className="inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium transition-colors hover:bg-accent"
            >
              <RotateCcw className="size-3.5" /> {t('Undo')}
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
