import type { ReactNode } from 'react'

// Shared between LogsPanel (single pod) and MultiPodLogsPanel (aggregated) —
// level detection/coloring, timestamp parsing and search highlighting are
// identical for both, only how lines are sourced differs.

export type Level = 'error' | 'warn' | 'info' | 'debug' | 'unknown'

export const LEVELS: Level[] = ['error', 'warn', 'info', 'debug']
export const LEVEL_COLOR: Record<Level, string> = {
  error: 'var(--err)',
  warn: 'var(--warn)',
  info: 'var(--ok)',
  debug: '#5aa2ff',
  unknown: 'transparent',
}
export const LEVEL_TEXT: Record<Level, string> = {
  error: 'text-[color:var(--err)]',
  warn: 'text-[color:var(--warn)]',
  info: 'text-[color:var(--ok)]',
  debug: 'text-[#5aa2ff]',
  unknown: 'text-muted-foreground',
}

export function detectLevel(msg: string): Level {
  const field = /(?:level|lvl|severity)["']?\s*[=:]\s*["']?(\w+)/i.exec(msg)
  const token = field?.[1] ?? /\b(ERROR|ERRO|FATAL|PANIC|WARN|WARNING|INFO|DEBUG|TRACE)\b/.exec(msg)?.[1]
  switch (token?.toLowerCase()) {
    case 'error':
    case 'erro':
    case 'fatal':
    case 'panic':
      return 'error'
    case 'warn':
    case 'warning':
      return 'warn'
    case 'info':
      return 'info'
    case 'debug':
    case 'trace':
      return 'debug'
    default:
      return 'unknown'
  }
}

// kubelet prepends an RFC3339Nano timestamp + space; split it off.
export function splitTimestamp(line: string): { ts?: number; msg: string } {
  const sp = line.indexOf(' ')
  if (sp > 0 && /^\d{4}-\d{2}-\d{2}T/.test(line.slice(0, sp))) {
    const t = Date.parse(line.slice(0, sp))
    if (!Number.isNaN(t)) return { ts: t, msg: line.slice(sp + 1) }
  }
  return { msg: line }
}

export function fmtLogTs(ms: number): string {
  const d = new Date(ms)
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

// Splits text around the (case-insensitive) search term for highlighting.
export function highlightText(text: string, term: string): ReactNode {
  if (!term) return text
  const lower = text.toLowerCase()
  const q = term.toLowerCase()
  const parts: ReactNode[] = []
  let i = 0
  let k = 0
  while (i < text.length) {
    const idx = lower.indexOf(q, i)
    if (idx === -1) {
      parts.push(text.slice(i))
      break
    }
    if (idx > i) parts.push(text.slice(i, idx))
    parts.push(
      <mark key={k++} className="rounded bg-[color:var(--warn)]/30 text-inherit">
        {text.slice(idx, idx + q.length)}
      </mark>,
    )
    i = idx + q.length
  }
  return parts
}

// Stable, distinct colors for tagging lines by source pod in the aggregated view.
const POD_COLORS = ['#5aa2ff', '#ff7a5a', '#5aff9d', '#e05aff', '#ffd35a', '#5affe0', '#ff5a8a', '#a3ff5a']
export function colorForPodIndex(i: number): string {
  return POD_COLORS[i % POD_COLORS.length]
}
