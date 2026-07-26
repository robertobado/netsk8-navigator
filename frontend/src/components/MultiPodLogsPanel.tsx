import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDownUp, Box, ChevronDown, Clock, Radio, Search, Trash2, WrapText } from 'lucide-react'
import { api, workloadLogsURL, type ManifestKind } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { detectLevel, fmtLogTs, highlightText, splitTimestamp, colorForPodIndex, LEVELS, LEVEL_COLOR, LEVEL_TEXT, type Level } from '@/lib/logs'
import { LogToolToggle } from './LogToolToggle'

interface MultiLogLine {
  id: number
  ts?: number
  level: Level
  msg: string
  pod: string
}

/**
 * Same viewer as LogsPanel, but fans in every pod of a workload into one
 * interleaved stream (backend: handleWorkloadLogs), tagging each line with
 * its source pod so it can be colored and filtered per pod.
 */
export function MultiPodLogsPanel({ ctx, kind, namespace, name }: Readonly<{ ctx: string; kind: ManifestKind; namespace: string; name: string }>) {
  const t = useT()
  const podsQ = useQuery({ queryKey: ['workloadpods', ctx, kind, namespace, name], queryFn: () => api.workloadPods(ctx, kind, namespace, name) })
  const podNames = useMemo(() => (podsQ.data ?? []).map((p) => p.name), [podsQ.data])
  const containers = useMemo(() => podsQ.data?.[0]?.containers ?? [], [podsQ.data])
  // Derived (not effect-driven) so the container resolves in the same render
  // the pod list arrives in — an effect-driven default would settle a render
  // late and reopen the EventSource below a second time on every mount.
  const [containerOverride, setContainerOverride] = useState<string | undefined>()
  const container = containerOverride ?? containers[0]

  const podColor = useMemo(() => {
    const m = new Map<string, string>()
    podNames.forEach((p, i) => m.set(p, colorForPodIndex(i)))
    return m
  }, [podNames])

  const [lines, setLines] = useState<MultiLogLine[]>([])
  const [search, setSearch] = useState('')
  const [wrap, setWrap] = useState(true)
  const [showTs, setShowTs] = useState(true)
  const [newest, setNewest] = useState(false)
  const [hiddenLevels, setHiddenLevels] = useState<Set<Level>>(new Set())
  const [hiddenPods, setHiddenPods] = useState<Set<string>>(new Set())
  const scroller = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const counter = useRef(0)

  useEffect(() => {
    if (podNames.length === 0) return
    setLines([])
    counter.current = 0
    const es = new EventSource(workloadLogsURL(ctx, kind, namespace, name, container))
    es.onmessage = (e) => {
      const { pod, line } = JSON.parse(e.data) as { pod: string; line: string }
      const { ts, msg } = splitTimestamp(line)
      const parsed: MultiLogLine = { id: counter.current++, ts, level: detectLevel(msg), msg, pod }
      setLines((prev) => (prev.length > 5000 ? [...prev.slice(-4000), parsed] : [...prev, parsed]))
    }
    return () => es.close()
  }, [ctx, kind, namespace, name, container, podNames.length])

  const counts = useMemo(() => {
    const c: Record<Level, number> = { error: 0, warn: 0, info: 0, debug: 0, unknown: 0 }
    for (const l of lines) c[l.level]++
    return c
  }, [lines])

  const view = useMemo(() => {
    const q = search.toLowerCase()
    let out = lines.filter((l) => !hiddenLevels.has(l.level) && !hiddenPods.has(l.pod) && (!q || l.msg.toLowerCase().includes(q)))
    if (newest) out = [...out].reverse()
    return out
  }, [lines, search, hiddenLevels, hiddenPods, newest])

  useEffect(() => {
    if (!newest && stick.current && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight
  }, [view, newest])

  const toggleLevel = (lv: Level) =>
    setHiddenLevels((prev) => {
      const next = new Set(prev)
      if (next.has(lv)) next.delete(lv)
      else next.add(lv)
      return next
    })
  const togglePod = (p: string) =>
    setHiddenPods((prev) => {
      const next = new Set(prev)
      if (next.has(p)) next.delete(p)
      else next.add(p)
      return next
    })

  if (podsQ.isLoading) {
    return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t('Loading...')}</div>
  }
  if (podNames.length === 0) {
    return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t('No pods for this workload.')}</div>
  }

  return (
    <div className="flex h-full flex-col bg-[#0b0e14]">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-white/5 px-3 py-2">
        <div className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-2">
          <Search className="size-3.5 text-muted-foreground" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('Search logs...')}
            className="w-44 bg-transparent py-1.5 text-xs outline-none placeholder:text-muted-foreground"
          />
        </div>

        {containers.length > 1 && (
          <div className="flex items-center gap-1.5">
            <Box className="size-3.5 text-muted-foreground" />
            <div className="relative">
              <select
                value={container}
                onChange={(e) => setContainerOverride(e.target.value)}
                className="appearance-none rounded-md border border-white/10 bg-white/5 py-1 pl-2 pr-6 text-xs outline-none"
              >
                {containers.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-1.5 top-1/2 size-3 -translate-y-1/2 text-muted-foreground" />
            </div>
          </div>
        )}

        <div className="flex items-center gap-1">
          {LEVELS.map((lv) => (
            <button
              key={lv}
              onClick={() => toggleLevel(lv)}
              title={`${counts[lv]} ${lv}`}
              className={cn(
                'flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] font-medium uppercase tracking-wide transition-opacity',
                hiddenLevels.has(lv) ? 'opacity-35' : '',
              )}
            >
              <span className="size-2 rounded-full" style={{ background: LEVEL_COLOR[lv] }} />
              <span className={LEVEL_TEXT[lv]}>{counts[lv]}</span>
            </button>
          ))}
        </div>

        <div className="ml-auto flex items-center gap-1">
          <LogToolToggle active={showTs} onClick={() => setShowTs((v) => !v)} icon={Clock} title="Timestamps" />
          <LogToolToggle active={wrap} onClick={() => setWrap((v) => !v)} icon={WrapText} title={t('Line wrap')} />
          <LogToolToggle active={newest} onClick={() => setNewest((v) => !v)} icon={ArrowDownUp} title={t('Newest first')} />
          <LogToolToggle active={false} onClick={() => setLines([])} icon={Trash2} title={t('Clear')} />
          <span className="ml-1 flex items-center gap-1 rounded-full bg-[color:var(--ok)]/12 px-2 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">
            <Radio className="size-2.5 animate-pulse" /> {view.length}
          </span>
        </div>
      </div>

      {/* Pod filter chips */}
      <div className="flex flex-wrap items-center gap-1.5 border-b border-white/5 px-3 py-1.5">
        {podNames.map((p) => (
          <button
            key={p}
            onClick={() => togglePod(p)}
            className={cn(
              'flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-mono transition-opacity',
              hiddenPods.has(p) ? 'border-white/10 opacity-40' : 'border-white/20',
            )}
          >
            <span className="size-1.5 rounded-full" style={{ background: podColor.get(p) }} />
            {p}
          </button>
        ))}
      </div>

      {/* Lines */}
      <div
        ref={scroller}
        onScroll={(e) => {
          const el = e.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        }}
        className="min-h-0 flex-1 overflow-auto py-1 font-mono text-xs leading-relaxed"
      >
        {view.length === 0 && <p className="px-3 py-6 text-muted-foreground">{lines.length ? t('No line matches the filter.') : t('Waiting for logs...')}</p>}
        {view.map((l) => (
          <div key={l.id} className="group flex gap-2 px-3 hover:bg-white/[0.04]">
            <span className="shrink-0 self-stretch border-l-2" style={{ borderColor: LEVEL_COLOR[l.level] }} />
            <span className="w-24 shrink-0 select-none truncate text-[10px] font-medium" style={{ color: podColor.get(l.pod) }} title={l.pod}>
              {l.pod}
            </span>
            {showTs && l.ts !== undefined && <span className="shrink-0 select-none tabular-nums text-slate-500">{fmtLogTs(l.ts)}</span>}
            <span className={cn('text-slate-300', wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre')}>{highlightText(l.msg, search)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
