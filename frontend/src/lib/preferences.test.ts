import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

// Each test needs a fresh module instance: `state` and the `hydrated` guard
// are singletons initialized at import time from localStorage, so a prior
// test's hydrate/setAppPrefs call would otherwise leak into the next one.
afterEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
  vi.resetModules()
})

describe('setAppPrefs / useAppPrefs', () => {
  it('merges a patch into state and persists it to localStorage', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    return import('./preferences').then(({ setAppPrefs, useAppPrefs }) => {
      const { result } = renderHook(() => useAppPrefs())
      act(() => setAppPrefs({ language: 'en' }))
      expect(result.current.language).toBe('en')
      expect(JSON.parse(localStorage.getItem('netsk8.prefs')!).language).toBe('en')
    })
  })

  it("defaults to the dark theme (pre-existing users keep today's look)", () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    return import('./preferences').then(({ useAppPrefs }) => {
      const { result } = renderHook(() => useAppPrefs())
      expect(result.current.theme).toBe('dark')
    })
  })

  it('reflects an explicit theme onto <html data-theme>, and clears it for "auto"', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    return import('./preferences').then(({ setAppPrefs }) => {
      act(() => setAppPrefs({ theme: 'light' }))
      expect(document.documentElement.dataset.theme).toBe('light')

      act(() => setAppPrefs({ theme: 'auto' }))
      expect(document.documentElement.dataset.theme).toBeUndefined()
    })
  })
})

describe('hydrateAppPrefs', () => {
  it('adopts well-typed fields from the server response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ language: 'en', metricsRefreshMs: 30_000 }) }))
    const { hydrateAppPrefs, useAppPrefs } = await import('./preferences')
    const { result } = renderHook(() => useAppPrefs())
    hydrateAppPrefs()
    await waitFor(() => expect(result.current.language).toBe('en'))
    expect(result.current.metricsRefreshMs).toBe(30_000)
  })

  it('discards wrongly-typed or unknown fields from a tampered response instead of trusting them', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          language: 123,
          metricsRefreshMs: 'not-a-number',
          evilField: '<script>alert(1)</script>',
          background: { enabled: 'yes', effect: 42, opacity: 'high' },
          theme: 'purple',
        }),
      }),
    )
    const { hydrateAppPrefs, useAppPrefs } = await import('./preferences')
    const { result } = renderHook(() => useAppPrefs())
    hydrateAppPrefs()
    // Every tainted field falls back to its default instead of being persisted as-is.
    await waitFor(() => expect(localStorage.getItem('netsk8.prefs')).not.toBeNull())
    expect(result.current.language).toBe('pt-BR')
    expect(result.current.metricsRefreshMs).toBe(15_000)
    expect(result.current.background).toEqual({ enabled: false, effect: 'net', opacity: 0.6 })
    expect(result.current.theme).toBe('dark') // "purple" isn't a real mode — falls back to the default
    expect((result.current as unknown as Record<string, unknown>).evilField).toBeUndefined()
  })

  it('defaults mcp.readDisabledContexts and contexts.favorites to empty arrays', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { useAppPrefs } = await import('./preferences')
    const { result } = renderHook(() => useAppPrefs())
    expect(result.current.mcp.readDisabledContexts).toEqual([])
    expect(result.current.contexts.favorites).toEqual([])
  })

  it('adopts mcp.readDisabledContexts and contexts.favorites from the server response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          mcp: { enabled: true, allowWrite: false, readOnlyContexts: [], readDisabledContexts: ['prod'] },
          contexts: { favorites: ['staging'] },
        }),
      }),
    )
    const { hydrateAppPrefs, useAppPrefs } = await import('./preferences')
    const { result } = renderHook(() => useAppPrefs())
    hydrateAppPrefs()
    await waitFor(() => expect(result.current.mcp.readDisabledContexts).toEqual(['prod']))
    expect(result.current.contexts.favorites).toEqual(['staging'])
  })

  it('discards a tampered contexts.favorites (non-string entries) instead of trusting it', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ contexts: { favorites: [1, 2, 'valid'] } }) }))
    const { hydrateAppPrefs, useAppPrefs } = await import('./preferences')
    const { result } = renderHook(() => useAppPrefs())
    hydrateAppPrefs()
    await waitFor(() => expect(result.current.contexts.favorites).toEqual(['valid']))
  })

  it('pushes local defaults to the server when it has no saved prefs yet', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    const { hydrateAppPrefs } = await import('./preferences')
    hydrateAppPrefs()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock).toHaveBeenLastCalledWith('/api/preferences', expect.objectContaining({ method: 'PUT' }))
  })

  it('only runs once even if called again', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    const { hydrateAppPrefs } = await import('./preferences')
    hydrateAppPrefs()
    hydrateAppPrefs()
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(fetchMock).toHaveBeenCalledTimes(2) // one GET + one PUT, not two of each
  })
})
