import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownUp, Clock, Radio, Search, Trash2, WrapText } from 'lucide-react'
import { logsURL } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import { detectLevel, fmtLogTs, highlightText, splitTimestamp, LEVELS, LEVEL_COLOR, LEVEL_TEXT, type Level } from '@/lib/logs'
import { LogToolToggle } from './LogToolToggle'

interface LogLine {
  id: number
  ts?: number // ms
  level: Level
  msg: string
}

/**
 * Grafana-style log viewer: streams pod logs over SSE with a timestamp column,
 * per-line level detection + color, search highlight, level filters and
 * wrap/order/timestamp toggles. Follows the tail unless scrolled up.
 */
export function LogsPanel({ ctx, namespace, pod, container }: Readonly<{ ctx: string; namespace: string; pod: string; container?: string }>) {
  const t = useT()
  const [lines, setLines] = useState<LogLine[]>([])
  const [search, setSearch] = useState('')
  const [wrap, setWrap] = useState(true)
  const [showTs, setShowTs] = useState(true)
  const [newest, setNewest] = useState(false)
  const [hidden, setHidden] = useState<Set<Level>>(new Set())
  const scroller = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const counter = useRef(0)

  useEffect(() => {
    setLines([])
    counter.current = 0
    const es = new EventSource(logsURL(ctx, namespace, pod, container))
    es.onmessage = (e) => {
      const { line } = JSON.parse(e.data) as { line: string }
      const parsed = parseLine(line, counter.current++)
      setLines((prev) => (prev.length > 5000 ? [...prev.slice(-4000), parsed] : [...prev, parsed]))
    }
    return () => es.close()
  }, [ctx, namespace, pod, container])

  const counts = useMemo(() => {
    const c: Record<Level, number> = { error: 0, warn: 0, info: 0, debug: 0, unknown: 0 }
    for (const l of lines) c[l.level]++
    return c
  }, [lines])

  const view = useMemo(() => {
    const q = search.toLowerCase()
    let out = lines.filter((l) => !hidden.has(l.level) && (!q || l.msg.toLowerCase().includes(q)))
    if (newest) out = [...out].reverse()
    return out
  }, [lines, search, hidden, newest])

  useEffect(() => {
    if (!newest && stick.current && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight
  }, [view, newest])

  const toggleLevel = (lv: Level) =>
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(lv)) next.delete(lv)
      else next.add(lv)
      return next
    })

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

        <div className="flex items-center gap-1">
          {LEVELS.map((lv) => (
            <button
              type="button"
              key={lv}
              onClick={() => toggleLevel(lv)}
              title={`${counts[lv]} ${lv}`}
              className={cn(
                'flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] font-medium uppercase tracking-wide transition-opacity',
                hidden.has(lv) ? 'opacity-35' : '',
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
            {showTs && l.ts !== undefined && <span className="shrink-0 select-none tabular-nums text-slate-500">{fmtLogTs(l.ts)}</span>}
            <span className={cn('text-slate-300', wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre')}>{highlightText(l.msg, search)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// kubelet prepends an RFC3339Nano timestamp + space; parseLine splits it off and detects level.
function parseLine(line: string, id: number): LogLine {
  const { ts, msg } = splitTimestamp(line)
  return { id, ts, level: detectLevel(msg), msg }
}
