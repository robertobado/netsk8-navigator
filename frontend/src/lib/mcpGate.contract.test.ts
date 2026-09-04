import { afterEach, beforeEach, describe, expect, inject, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

// Contract test for mcpGate.ts against the REAL Go backend — see
// testsupport/gateServerGlobalSetup.ts, which spawns cmd/gateserver (the
// actual api.Server routes + config.Store, over a real HTTP listener and a
// disposable config.json) and hands us its URL via `inject`.
//
// mcpGate.test.ts already covers the store's own logic against a stubbed
// fetch. That stub is written from our own understanding of the backend's
// contract, so it can't catch a divergence from the real one — a wire-shape
// mismatch, mergeGate handling a partial patch differently than assumed, or
// the awaited-PUT ordering guarantee not actually holding once a real
// network hop and a real handler are in the loop. This file exists to catch
// exactly that class of bug, the one the v0.0.27 desync (see mcpGate.ts's
// header comment) was.
//
// Run with `pnpm test:contract` (needs the Go toolchain on PATH; excluded
// from the default `pnpm test` run — see vitest.contract.config.ts).

const baseUrl = inject('gateServerUrl')

function gateUrl() {
  return `${baseUrl}/api/mcp/gate`
}

async function resetGate() {
  await fetch(gateUrl(), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] }),
  })
}

// mcpGate.ts fetches a same-origin relative path; redirect it at the real
// gateserver instead of stubbing the response ourselves.
function pointFetchAtGateServer() {
  const realFetch = fetch
  vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' && input.startsWith('/') ? `${baseUrl}${input}` : input
    return realFetch(url, init)
  })
}

describe('mcpGate.ts against the real backend', () => {
  beforeEach(async () => {
    localStorage.clear()
    vi.resetModules()
    await resetGate()
    pointFetchAtGateServer()
  })

  afterEach(async () => {
    vi.unstubAllGlobals()
    await resetGate()
  })

  it('round-trips a partial patch through the real handler and config store', async () => {
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    await act(() => setMcpGate({ enabled: true, allowWrite: true }))
    expect(result.current).toEqual({ enabled: true, allowWrite: true, readOnlyContexts: [], readDisabledContexts: [] })

    // A second, independent patch merges onto the first instead of replacing it.
    await act(() => setMcpGate({ readOnlyContexts: ['prod'] }))
    expect(result.current).toEqual({ enabled: true, allowWrite: true, readOnlyContexts: ['prod'], readDisabledContexts: [] })
  })

  it('enforces the enabled/allowWrite invariant on the real, persisted bytes', async () => {
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    await act(() => setMcpGate({ enabled: true, allowWrite: true }))
    await act(() => setMcpGate({ enabled: false }))
    expect(result.current.allowWrite).toBe(false)

    // Re-enabling must not resurrect the old allowWrite — the backend has to
    // have actually forgotten it (canonicalGate on the stored bytes), not
    // just the in-memory flags.
    await act(() => setMcpGate({ enabled: true }))
    expect(result.current.allowWrite).toBe(false)
  })

  it('two awaited toggles in a row land on the server in order', async () => {
    const { setMcpGate } = await import('./mcpGate')

    await act(async () => {
      await setMcpGate({ enabled: true })
      await setMcpGate({ allowWrite: true })
    })

    const res = await fetch(gateUrl())
    expect(await res.json()).toMatchObject({ enabled: true, allowWrite: true })
  })

  it('hydrateMcpGate adopts the server state over a stale localStorage', async () => {
    localStorage.setItem('netsk8.mcpgate', JSON.stringify({ enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] }))
    await fetch(gateUrl(), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: true, allowWrite: true, readOnlyContexts: ['prod'] }),
    })

    const { hydrateMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())
    hydrateMcpGate()

    await waitFor(() => expect(result.current.enabled).toBe(true))
    expect(result.current.readOnlyContexts).toEqual(['prod'])
  })

  it('rejects and leaves local state untouched on a real 400 (malformed patch)', async () => {
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    // readOnlyContexts must be an array of strings server-side; send a
    // number instead. mergeGate's json.Unmarshal of the patch fails closed.
    const realFetch = fetch
    vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'PUT') return realFetch(gateUrl(), { ...init, body: '{"readOnlyContexts": 5}' })
      const url = typeof input === 'string' && input.startsWith('/') ? `${baseUrl}${input}` : input
      return realFetch(url, init)
    })

    await expect(setMcpGate({ enabled: true })).rejects.toThrow(/400/)
    expect(result.current.enabled).toBe(false)
  })
})
