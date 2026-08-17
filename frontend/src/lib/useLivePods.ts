import { useEffect, useRef, useState } from 'react'
import type { Pod } from './api'

type ConnState = 'connecting' | 'live' | 'error'

interface PodEvent {
  type: 'ADDED' | 'MODIFIED' | 'DELETED' | 'SYNCED'
  object?: Pod
}

const key = (p: Pod) => `${p.namespace}/${p.name}`
const enc = (s: string) => encodeURIComponent(s)
const nsQuery = (namespace?: string) => (namespace ? `?namespace=${enc(namespace)}` : '')

/**
 * Subscribes to the backend pod watch stream (SSE) and maintains a live map of
 * pods keyed by namespace/name. ADDED/MODIFIED upsert, DELETED removes. Each
 * (re)connection replays a fresh snapshot, so we rebuild into a staging map and
 * swap it in on SYNCED — reconnects never flash empty or leave stale rows.
 */
// Deltas are coalesced: a busy cluster streams many MODIFIED events per second,
// and re-deriving the array (→ full table re-render + sort/filter recompute) on
// each one pins the CPU. We instead flush at most once per FLUSH_MS.
const FLUSH_MS = 700

export function useLivePods(ctx?: string, namespace = '') {
  const [pods, setPods] = useState<Pod[]>([])
  const [state, setState] = useState<ConnState>('connecting')
  const stage = useRef<Map<string, Pod>>(new Map())
  const synced = useRef(false)

  useEffect(() => {
    if (!ctx) return
    setState('connecting')
    setPods([])
    stage.current = new Map()
    synced.current = false

    // Coalesce staged deltas into one state update per window.
    let flushTimer: ReturnType<typeof setTimeout> | undefined
    let dirty = false
    const flush = () => {
      flushTimer = undefined
      if (!dirty) return
      dirty = false
      setPods([...stage.current.values()])
    }
    const scheduleFlush = () => {
      dirty = true
      flushTimer ??= setTimeout(flush, FLUSH_MS)
    }

    const url = `/api/contexts/${enc(ctx)}/watch/pods${nsQuery(namespace)}`
    const es = new EventSource(url)

    es.onopen = () => {
      // A new snapshot is on its way — stage it before swapping.
      stage.current = new Map()
      synced.current = false
    }

    es.onmessage = (e) => {
      const ev: PodEvent = JSON.parse(e.data)
      if (ev.type === 'SYNCED') {
        synced.current = true
        setState('live')
        setPods([...stage.current.values()]) // initial snapshot: show at once
        return
      }
      if (!ev.object) return
      if (ev.type === 'DELETED') stage.current.delete(key(ev.object))
      else stage.current.set(key(ev.object), ev.object)
      // After the initial sync, batch deltas rather than re-render per event.
      if (synced.current) scheduleFlush()
    }

    es.onerror = () => {
      setState('error') // EventSource auto-reconnects; onopen will resync.
    }

    return () => {
      es.close()
      if (flushTimer) clearTimeout(flushTimer)
    }
  }, [ctx, namespace])

  return { pods, state }
}
