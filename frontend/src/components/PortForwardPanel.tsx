import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, Network, Square } from 'lucide-react'
import { getDetail, listPortForwards, startPortForward, stopPortForward } from '@/lib/api'
import { useT } from '@/lib/i18n'

// Lists a pod's container ports and lets the user open/close a local,
// loopback-only tunnel to each — same model as `kubectl port-forward`: the
// client connecting to 127.0.0.1:<localPort> runs on this machine, not on
// whatever device is remotely viewing this UI.
export function PortForwardPanel({ ctx, namespace, name }: Readonly<{ ctx: string; namespace: string; name: string }>) {
  const t = useT()
  const qc = useQueryClient()
  // Same queryKey DetailView/ResourceActions use for this pod — dedupes the request.
  const detailQ = useQuery({ queryKey: ['detail', ctx, 'pod', namespace, name], queryFn: () => getDetail(ctx, 'pod', namespace, name) })
  const sessionsQ = useQuery({ queryKey: ['portforwards', ctx], queryFn: () => listPortForwards(ctx), refetchInterval: 4000 })

  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [error, setError] = useState('')

  const ports = detailQ.data?.ports ?? []
  const sessions = sessionsQ.data ?? []
  const sessionFor = (port: string) => sessions.find((s) => s.namespace === namespace && s.pod === name && String(s.port) === port)

  const start = async (port: string) => {
    setBusyKey(port)
    setError('')
    try {
      await startPortForward(ctx, namespace, name, Number(port))
      await qc.invalidateQueries({ queryKey: ['portforwards', ctx] })
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusyKey(null)
    }
  }

  const stop = async (id: string) => {
    setBusyKey(id)
    setError('')
    try {
      await stopPortForward(ctx, id)
      await qc.invalidateQueries({ queryKey: ['portforwards', ctx] })
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusyKey(null)
    }
  }

  if (detailQ.isLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> {t('Loading...')}
      </div>
    )
  }

  if (ports.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">{t('This pod exposes no container ports.')}</div>
    )
  }

  return (
    <div className="space-y-3 overflow-y-auto p-4">
      {error && <p className="text-xs text-[color:var(--err)]">{error}</p>}
      {ports.map((p) => {
        const sess = sessionFor(p.port)
        const key = `${p.extra}-${p.port}`
        return (
          <div key={key} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-card/60 px-4 py-3 backdrop-blur-xl">
            <div className="min-w-0">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Network className="size-4 text-muted-foreground" />
                {p.port}
                {p.name && <span className="text-xs font-normal text-muted-foreground">({p.name})</span>}
              </div>
              <p className="text-xs text-muted-foreground">
                {p.extra} · {p.protocol || 'TCP'}
              </p>
            </div>
            {sess ? (
              <div className="flex items-center gap-2">
                <CopyableAddress address={`127.0.0.1:${sess.localPort}`} />
                <button
                  onClick={() => stop(sess.id)}
                  disabled={busyKey === sess.id}
                  className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-[color:var(--err)] transition-colors hover:bg-[color:var(--err)]/10 disabled:opacity-50"
                >
                  {busyKey === sess.id ? <Loader2 className="size-3.5 animate-spin" /> : <Square className="size-3.5" />}
                  {t('Stop')}
                </button>
              </div>
            ) : (
              <button
                onClick={() => start(p.port)}
                disabled={busyKey === p.port}
                className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {busyKey === p.port && <Loader2 className="size-4 animate-spin" />}
                {t('Start forwarding')}
              </button>
            )}
          </div>
        )
      })}
    </div>
  )
}

function CopyableAddress({ address }: Readonly<{ address: string }>) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(address)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard blocked — ignore */
    }
  }
  return (
    <button
      onClick={copy}
      className="inline-flex items-center gap-1.5 rounded-lg border bg-background/50 px-2.5 py-1.5 font-mono text-xs transition-colors hover:bg-accent"
    >
      {address}
      {copied ? <Check className="size-3.5 text-[color:var(--ok)]" /> : <Copy className="size-3.5" />}
    </button>
  )
}
