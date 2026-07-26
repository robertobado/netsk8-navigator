import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Package, PackagePlus, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { addHelmRepo, helmRepos, refreshHelmRepo, removeHelmRepo } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { HelmInstallDialogLazy } from './HelmInstallDialogLazy'

export function HelmReposPanel({ ctx }: Readonly<{ ctx: string }>) {
  const t = useT()
  const qc = useQueryClient()
  const reposQ = useQuery({ queryKey: ['helmRepos'], queryFn: helmRepos })
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState('')
  const [busyRepo, setBusyRepo] = useState<string | null>(null)
  const [installOpen, setInstallOpen] = useState(false)

  const invalidate = () => qc.invalidateQueries({ queryKey: ['helmRepos'] })

  const addRepo = async (e: React.FormEvent) => {
    e.preventDefault()
    setAdding(true)
    setError('')
    try {
      await addHelmRepo(name, url)
      setName('')
      setUrl('')
      invalidate()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setAdding(false)
    }
  }

  const refresh = async (repoName: string) => {
    setBusyRepo(repoName)
    try {
      await refreshHelmRepo(repoName)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusyRepo(null)
    }
  }

  const remove = async (repoName: string) => {
    setBusyRepo(repoName)
    try {
      await removeHelmRepo(repoName)
      invalidate()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusyRepo(null)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto">
      <div className="rounded-2xl border bg-card/60 p-4 backdrop-blur-xl">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">{t('Helm repositories')}</h3>
          <button
            onClick={() => setInstallOpen(true)}
            disabled={(reposQ.data?.length ?? 0) === 0}
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
          >
            <PackagePlus className="size-4" /> {t('Install chart')}
          </button>
        </div>

        <form onSubmit={addRepo} className="mb-3 flex flex-wrap gap-2">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('Repo name')}
            className="w-36 rounded-md border bg-background/50 px-2.5 py-1.5 text-sm outline-none"
          />
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://charts.example.com"
            className="min-w-[14rem] flex-1 rounded-md border bg-background/50 px-2.5 py-1.5 text-sm outline-none"
          />
          <button
            type="submit"
            disabled={adding || !name.trim() || !url.trim()}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
              !adding && name.trim() && url.trim()
                ? 'bg-primary text-primary-foreground hover:opacity-90'
                : 'cursor-not-allowed bg-muted text-muted-foreground',
            )}
          >
            {adding ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
            {t('Add')}
          </button>
        </form>
        {error && <p className="mb-2 text-xs text-[color:var(--err)]">{error}</p>}

        <div className="space-y-1.5">
          {reposQ.isLoading && (
            <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" /> {t('Loading...')}
            </div>
          )}
          {reposQ.data?.length === 0 && <p className="py-2 text-xs text-muted-foreground">{t('No repositories added yet.')}</p>}
          {reposQ.data?.map((r) => (
            <div key={r.name} className="flex items-center gap-3 rounded-lg border bg-background/40 px-3 py-2">
              <Package className="size-4 shrink-0 text-muted-foreground" />
              <span className="shrink-0 font-medium text-sm">{r.name}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">{r.url}</span>
              <button
                onClick={() => refresh(r.name)}
                disabled={busyRepo === r.name}
                title={t('Refresh')}
                className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50"
              >
                <RefreshCw className={cn('size-3.5', busyRepo === r.name && 'animate-spin')} />
              </button>
              <button
                onClick={() => remove(r.name)}
                disabled={busyRepo === r.name}
                title={t('Remove')}
                className="rounded-md p-1.5 text-[color:var(--err)] transition-colors hover:bg-[color:var(--err)]/10 disabled:opacity-50"
              >
                <Trash2 className="size-3.5" />
              </button>
            </div>
          ))}
        </div>
      </div>

      <HelmInstallDialogLazy ctx={ctx} mode="install" open={installOpen} onClose={() => setInstallOpen(false)} onDone={() => setInstallOpen(false)} />
    </div>
  )
}
