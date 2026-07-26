import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { REFRESH_OPTIONS, setMetricsRefresh, useMetricsRefresh } from './metrics'

// setAppPrefs mirrors preferences to the backend via fetch — stub it so these
// stay pure localStorage/state tests with no network involved.
vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

afterEach(() => {
  localStorage.clear()
})

describe('REFRESH_OPTIONS', () => {
  it('includes an Off option at 0ms', () => {
    expect(REFRESH_OPTIONS[0]).toEqual({ ms: 0, label: 'Off' })
  })
})

describe('useMetricsRefresh', () => {
  it('reports interval=null when off', () => {
    setMetricsRefresh(0)
    const { result } = renderHook(() => useMetricsRefresh())
    expect(result.current.ms).toBe(0)
    expect(result.current.interval).toBeNull()
  })

  it('reports the interval in ms when on', () => {
    setMetricsRefresh(15_000)
    const { result } = renderHook(() => useMetricsRefresh())
    expect(result.current.ms).toBe(15_000)
    expect(result.current.interval).toBe(15_000)
  })
})
