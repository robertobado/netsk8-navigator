import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Ban, Check, CheckCircle2, History, Loader2, RefreshCw, Scaling, Trash2 } from 'lucide-react'
import {
  cordonNode,
  deleteResource,
  getDetail,
  restartRollout,
  scaleResource,
  HISTORY_KINDS,
  RESTARTABLE_KINDS,
  SCALABLE_KINDS,
  type ManifestKind,
} from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { RolloutHistory } from './RolloutHistory'

interface ActionProps {
  ctx: string
  kind: ManifestKind
  namespace: string
  name: string
}

// Mutating action bar for a resource drawer: scale (deployments/statefulsets/
// replicasets), restart rollout (deployments/statefulsets/daemonsets), and
// delete (any kind). Renders nothing when the drawer isn't editable — the
// same gate ManifestPanel uses for read-only drill-downs.
export function ResourceActions({ ctx, kind, namespace, name, editable, onDeleted }: Readonly<ActionProps & { editable: boolean; onDeleted?: () => void }>) {
  const qc = useQueryClient()
  if (!editable) return null

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['resources'] })
    qc.invalidateQueries({ queryKey: ['detail'] })
    qc.invalidateQueries({ queryKey: ['workloadpods'] })
    qc.invalidateQueries({ queryKey: ['overview'] })
    qc.invalidateQueries({ queryKey: ['issues'] })
  }

  return (
    <div className="flex flex-wrap items-center gap-3 border-b px-5 py-2.5">
      {SCALABLE_KINDS.has(kind) && <ScaleAction ctx={ctx} kind={kind} namespace={namespace} name={name} onDone={invalidate} />}
      {RESTARTABLE_KINDS.has(kind) && <RestartAction ctx={ctx} kind={kind} namespace={namespace} name={name} onDone={invalidate} />}
      {kind === 'node' && <CordonAction ctx={ctx} kind={kind} namespace={namespace} name={name} onDone={invalidate} />}
      {HISTORY_KINDS.has(kind) && <HistoryAction ctx={ctx} kind={kind} namespace={namespace} name={name} />}
      <DeleteAction
        ctx={ctx}
        kind={kind}
        namespace={namespace}
        name={name}
        onDeleted={() => {
          invalidate()
          onDeleted?.()
        }}
      />
    </div>
  )
}

function DeleteAction({ ctx, kind, namespace, name, onDeleted }: Readonly<ActionProps & { onDeleted: () => void }>) {
  const t = useT()
  const [confirming, setConfirming] = useState(false)
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const cancel = () => {
    setConfirming(false)
    setInput('')
    setError('')
  }

  const del = async () => {
    setBusy(true)
    setError('')
    try {
      await deleteResource(ctx, kind, namespace, name)
      onDeleted()
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  if (!confirming) {
    return (
      <button
        onClick={() => setConfirming(true)}
        className="ml-auto inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-[color:var(--err)] transition-colors hover:bg-[color:var(--err)]/10"
      >
        <Trash2 className="size-4" /> {t('Delete')}
      </button>
    )
  }

  return (
    <div className="ml-auto flex flex-wrap items-center gap-2 rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-3 py-2">
      <AlertTriangle className="size-4 shrink-0 text-[color:var(--err)]" />
      <div className="flex flex-col gap-0.5">
        <span className="text-xs font-medium text-[color:var(--err)]">{t('Delete this resource permanently?')}</span>
        <span className="text-xs text-muted-foreground">
          {t('Type the name to confirm:')} <span className="font-mono text-foreground">{name}</span>
        </span>
      </div>
      <input
        value={input}
        onChange={(e) => setInput(e.target.value)}
        placeholder={name}
        aria-label={t('Type the name to confirm:')}
        className="w-40 rounded-md border bg-background/50 px-2 py-1 font-mono text-xs outline-none"
      />
      <button
        onClick={del}
        disabled={input !== name || busy}
        className="inline-flex items-center gap-1.5 rounded-lg bg-[color:var(--err)]/90 px-3 py-1.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
      >
        {busy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
        {t('Confirm')}
      </button>
      <button onClick={cancel} className="rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground">
        {t('Cancel')}
      </button>
      {error && <span className="w-full text-xs text-[color:var(--err)]">{error}</span>}
    </div>
  )
}

function ScaleAction({ ctx, kind, namespace, name, onDone }: Readonly<ActionProps & { onDone: () => void }>) {
  const t = useT()
  // Same queryKey DetailView.tsx uses for this resource — react-query dedupes
  // the request instead of firing a second one for this action bar.
  const q = useQuery({ queryKey: ['detail', ctx, kind, namespace, name], queryFn: () => getDetail(ctx, kind, namespace, name) })
  const [value, setValue] = useState('')
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  useEffect(() => {
    if (q.data?.replicas != null && value === '') setValue(String(q.data.replicas))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data])

  const n = Number(value)
  const valid = value !== '' && Number.isInteger(n) && n >= 0
  const dirty = valid && n !== q.data?.replicas

  const scale = async () => {
    setBusy(true)
    setError('')
    try {
      await scaleResource(ctx, kind, namespace, name, n)
      setConfirming(false)
      setDone(true)
      onDone()
      setTimeout(() => setDone(false), 3000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Scaling className="size-4 text-muted-foreground" />
      <span className="text-xs text-muted-foreground">{t('Replicas')}</span>
      <input
        type="number"
        min={0}
        value={value}
        onChange={(e) => {
          setValue(e.target.value)
          setConfirming(false)
        }}
        aria-label={t('Replicas')}
        className="w-16 rounded-md border bg-background/50 px-2 py-1 text-sm outline-none"
      />
      {done ? (
        <span className="inline-flex items-center gap-1 text-xs text-[color:var(--ok)]">
          <Check className="size-3.5" /> {t('Scaled')}
        </span>
      ) : confirming ? (
        <>
          <span className="text-xs text-[color:var(--warn)]">{t('Apply this scale to the live cluster?')}</span>
          <button
            onClick={scale}
            disabled={busy}
            className="inline-flex items-center gap-1 rounded-lg bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
          >
            {busy ? <Loader2 className="size-3.5 animate-spin" /> : t('Confirm')}
          </button>
          <button onClick={() => setConfirming(false)} className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
            {t('Cancel')}
          </button>
        </>
      ) : (
        <button
          onClick={() => setConfirming(true)}
          disabled={!dirty}
          className={cn(
            'rounded-lg px-2.5 py-1 text-xs font-medium',
            dirty ? 'bg-primary text-primary-foreground hover:opacity-90' : 'cursor-not-allowed bg-muted text-muted-foreground',
          )}
        >
          {t('Scale')}
        </button>
      )}
      {error && <span className="text-xs text-[color:var(--err)]">{error}</span>}
    </div>
  )
}

function RestartAction({ ctx, kind, namespace, name, onDone }: Readonly<ActionProps & { onDone: () => void }>) {
  const t = useT()
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  const restart = async () => {
    setBusy(true)
    setError('')
    try {
      await restartRollout(ctx, kind, namespace, name)
      setConfirming(false)
      setDone(true)
      onDone()
      setTimeout(() => setDone(false), 3000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (done) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-[color:var(--ok)]">
        <Check className="size-4" /> {t('Restart triggered')}
      </span>
    )
  }

  if (confirming) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-[color:var(--warn)]">{t('Restart this rollout now?')}</span>
        <button
          onClick={restart}
          disabled={busy}
          className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : t('Confirm')}
        </button>
        <button onClick={() => setConfirming(false)} className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
          {t('Cancel')}
        </button>
        {error && <span className="text-xs text-[color:var(--err)]">{error}</span>}
      </div>
    )
  }

  return (
    <button
      onClick={() => setConfirming(true)}
      className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      <RefreshCw className="size-4" /> {t('Restart rollout')}
    </button>
  )
}

function CordonAction({ ctx, kind, namespace, name, onDone }: Readonly<ActionProps & { onDone: () => void }>) {
  const t = useT()
  // Same queryKey the drawer's Details tab uses — react-query dedupes the
  // request instead of firing a second one just to read the current state.
  const q = useQuery({ queryKey: ['detail', ctx, kind, namespace, name], queryFn: () => getDetail(ctx, kind, namespace, name) })
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState<'cordoned' | 'uncordoned' | null>(null)

  const schedulable = q.data?.schedulable ?? true
  const cordon = async () => {
    setBusy(true)
    setError('')
    try {
      await cordonNode(ctx, name, schedulable)
      setConfirming(false)
      setDone(schedulable ? 'cordoned' : 'uncordoned')
      onDone()
      q.refetch()
      setTimeout(() => setDone(null), 3000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (done) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-[color:var(--ok)]">
        <Check className="size-4" /> {done === 'cordoned' ? t('Node cordoned') : t('Node uncordoned')}
      </span>
    )
  }

  if (confirming) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-[color:var(--warn)]">{schedulable ? t('Cordon this node now?') : t('Uncordon this node now?')}</span>
        <button
          onClick={cordon}
          disabled={busy}
          className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : t('Confirm')}
        </button>
        <button onClick={() => setConfirming(false)} className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
          {t('Cancel')}
        </button>
        {error && <span className="text-xs text-[color:var(--err)]">{error}</span>}
      </div>
    )
  }

  return (
    <button
      onClick={() => setConfirming(true)}
      className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      {schedulable ? <Ban className="size-4" /> : <CheckCircle2 className="size-4" />}
      {schedulable ? t('Cordon') : t('Uncordon')}
    </button>
  )
}

function HistoryAction({ ctx, namespace, name }: Readonly<ActionProps>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <History className="size-4" /> {t('History')}
      </button>
      <RolloutHistory ctx={ctx} namespace={namespace} name={name} open={open} onClose={() => setOpen(false)} />
    </>
  )
}
