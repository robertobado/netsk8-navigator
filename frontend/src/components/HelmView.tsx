import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import { Boxes, RefreshCw, ServerCrash, Store } from 'lucide-react'
import { helmReleases, type HelmRelease } from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { DataTable } from './DataTable'
import { HelmReleaseDrawer } from './HelmReleaseDrawer'
import { HelmReposPanel } from './HelmReposPanel'

// Helm's own release statuses (not a k8s phase), colored the same way
// StatusBadge colors k8s phases but matching Helm's actual vocabulary
// ("deployed", "pending-install", "failed", ...).
function helmStatusTone(status: string): 'ok' | 'warn' | 'err' | 'muted' {
  const s = status.toLowerCase()
  if (s === 'deployed') return 'ok'
  if (s.startsWith('pending')) return 'warn'
  if (s === 'failed') return 'err'
  return 'muted'
}
const TONE_STYLE: Record<string, string> = {
  ok: 'bg-[color:var(--ok)]/12 text-[color:var(--ok)] ring-[color:var(--ok)]/25',
  warn: 'bg-[color:var(--warn)]/12 text-[color:var(--warn)] ring-[color:var(--warn)]/25',
  err: 'bg-[color:var(--err)]/12 text-[color:var(--err)] ring-[color:var(--err)]/25',
  muted: 'bg-muted text-muted-foreground ring-border',
}
export function HelmStatusBadge({ status }: Readonly<{ status: string }>) {
  const tone = helmStatusTone(status)
  return <span className={cn('inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset', TONE_STYLE[tone])}>{status}</span>
}

type Tab = 'releases' | 'repos'

export function HelmView({ ctx, ns }: Readonly<{ ctx: string; ns: string }>) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('releases')
  return (
    <div className="flex h-full flex-col gap-4">
      <div className="flex gap-1">
        <TabButton active={tab === 'releases'} onClick={() => setTab('releases')} icon={Boxes} label={t('Releases')} />
        <TabButton active={tab === 'repos'} onClick={() => setTab('repos')} icon={Store} label={t('Repositories')} />
      </div>
      {tab === 'releases' ? <HelmReleasesTable ctx={ctx} ns={ns} /> : <HelmReposPanel ctx={ctx} />}
    </div>
  )
}

function TabButton({ active, onClick, icon: Icon, label }: Readonly<{ active: boolean; onClick: () => void; icon: typeof Boxes; label: string }>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="size-4" />
      {label}
    </button>
  )
}

type Row = HelmRelease
const hc = createColumnHelper<Row>()
const helmCols = [
  hc.accessor('name', { header: 'Name', cell: (c) => <span className="font-medium">{c.getValue()}</span> }),
  hc.accessor('namespace', { header: 'Namespace', cell: (c) => <span className="text-muted-foreground">{c.getValue()}</span> }),
  hc.accessor('chart', { header: 'Chart', cell: (c) => <span className="font-mono text-sm">{c.getValue()}</span> }),
  hc.accessor('appVersion', { header: 'App version', cell: (c) => <span className="font-mono text-sm text-muted-foreground">{c.getValue() || '—'}</span> }),
  hc.accessor('revision', { header: 'Revision', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  hc.accessor('status', { header: 'Status', cell: (c) => <HelmStatusBadge status={c.getValue()} /> }),
  hc.accessor('updated', {
    header: 'Updated',
    cell: (c) => <span className="text-sm text-muted-foreground">{age(c.getValue())}</span>,
    sortFn: (a, b) => new Date(a.original.updated).getTime() - new Date(b.original.updated).getTime(),
  }),
] as ColumnDef<Row, unknown>[]

function HelmReleasesTable({ ctx, ns }: Readonly<{ ctx: string; ns: string }>) {
  const t = useT()
  const qc = useQueryClient()
  const [target, setTarget] = useState<{ namespace: string; name: string } | null>(null)
  const q = useQuery({ queryKey: ['helmReleases', ctx, ns], queryFn: () => helmReleases(ctx, ns || undefined) })

  if (q.isError) {
    return (
      <div className="flex h-56 flex-col items-center justify-center gap-2 rounded-2xl border bg-card/60 text-center backdrop-blur-xl">
        <ServerCrash className="size-6 text-[color:var(--err)]" />
        <p className="text-sm font-medium text-[color:var(--err)]">{t('Could not load Helm releases.')}</p>
        <p className="max-w-lg truncate px-4 font-mono text-[10px] text-muted-foreground/60">{(q.error as Error).message}</p>
        <button
          type="button"
          onClick={() => q.refetch()}
          disabled={q.isFetching}
          className="mt-1 inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors hover:bg-accent disabled:opacity-50"
        >
          <RefreshCw className={cn('size-3.5', q.isFetching && 'animate-spin')} /> {t('Try again')}
        </button>
      </div>
    )
  }

  return (
    <>
      <DataTable
        title={t('Releases')}
        data={q.data ?? []}
        columns={helmCols}
        loading={q.isLoading}
        storageKey="helm-releases"
        facets={['namespace', 'status']}
        onRowClick={(row) => setTarget({ namespace: row.namespace, name: row.name })}
      />
      <HelmReleaseDrawer
        ctx={ctx}
        target={target}
        onClose={() => setTarget(null)}
        onChanged={() => qc.invalidateQueries({ queryKey: ['helmReleases', ctx] })}
      />
    </>
  )
}
