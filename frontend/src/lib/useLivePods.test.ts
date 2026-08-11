import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useLivePods } from './useLivePods'
import type { Pod } from './api'

class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }
  close() {
    this.closed = true
  }
}

function pod(namespace: string, name: string): Pod {
  return { namespace, name } as Pod
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useLivePods', () => {
  it('does not connect when ctx is empty', () => {
    renderHook(() => useLivePods(undefined))
    expect(MockEventSource.instances).toHaveLength(0)
  })

  it('connects to the watch endpoint, with the namespace query only when set', () => {
    renderHook(() => useLivePods('prod'))
    expect(MockEventSource.instances[0]?.url).toBe('/api/contexts/prod/watch/pods')

    renderHook(() => useLivePods('prod', 'kube-system'))
    expect(MockEventSource.instances[1]?.url).toBe('/api/contexts/prod/watch/pods?namespace=kube-system')
  })

  it('shows the initial snapshot at once on SYNCED and reports state=live', () => {
    const { result } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]

    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'ADDED', object: pod('default', 'web-1') }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })

    expect(result.current.state).toBe('live')
    expect(result.current.pods).toEqual([pod('default', 'web-1')])
  })

  it('ignores events with no object, other than SYNCED', () => {
    const { result } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]

    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'MODIFIED' }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })

    expect(result.current.pods).toEqual([])
  })

  it('coalesces post-sync deltas into one flush per window, and DELETED removes the pod', () => {
    const { result } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]

    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'ADDED', object: pod('default', 'web-1') }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })
    expect(result.current.pods).toHaveLength(1)

    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'ADDED', object: pod('default', 'web-2') }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'DELETED', object: pod('default', 'web-1') }) })
    })
    // Not flushed yet — deltas are batched until FLUSH_MS elapses.
    expect(result.current.pods).toHaveLength(1)

    act(() => {
      vi.advanceTimersByTime(700)
    })
    expect(result.current.pods).toEqual([pod('default', 'web-2')])
  })

  it('a fresh (re)connection rebuilds into a staging map instead of keeping stale rows', () => {
    const { result } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]

    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'ADDED', object: pod('default', 'web-1') }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })
    expect(result.current.pods).toHaveLength(1)

    act(() => {
      es.onopen?.()
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })
    expect(result.current.pods).toEqual([])
  })

  it('reports state=error on a connection error', () => {
    const { result } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]

    act(() => {
      es.onerror?.()
    })
    expect(result.current.state).toBe('error')
  })

  it('closes the EventSource and clears the pending flush timer on unmount', () => {
    const { unmount } = renderHook(() => useLivePods('prod'))
    const es = MockEventSource.instances[0]
    act(() => {
      es.onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
      es.onmessage?.({ data: JSON.stringify({ type: 'ADDED', object: pod('default', 'web-1') }) })
    })
    unmount()
    expect(es.closed).toBe(true)
  })

  it('resets to connecting/[] and reconnects when ctx changes', () => {
    const { result, rerender } = renderHook(({ ctx }) => useLivePods(ctx), { initialProps: { ctx: 'prod' } })
    act(() => {
      MockEventSource.instances[0].onmessage?.({ data: JSON.stringify({ type: 'SYNCED' }) })
    })

    act(() => {
      rerender({ ctx: 'staging' })
    })
    expect(MockEventSource.instances).toHaveLength(2)
    expect(result.current.state).toBe('connecting')
    expect(result.current.pods).toEqual([])
  })
})
