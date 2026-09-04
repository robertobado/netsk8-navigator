import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

// These tests stub fetch, so they cover this module's own logic but not
// whether it actually matches the backend's contract. mcpGate.contract.test.ts
// covers that half, against the real handler + config.Store (`pnpm test:contract`).
//
// `state` and the `hydrated` guard are module-level singletons initialised
// at import time, so each test takes a fresh module instance.
afterEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
  vi.resetModules()
})

function stubGateFetch(initial = { enabled: false, allowWrite: false, readOnlyContexts: [] as string[], readDisabledContexts: [] as string[] }) {
  let store = { ...initial }
  const canonical = (g: typeof store) => {
    const enabled = !!g.enabled
    return { enabled, allowWrite: enabled && !!g.allowWrite, readOnlyContexts: g.readOnlyContexts ?? [], readDisabledContexts: g.readDisabledContexts ?? [] }
  }
  const fn = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') store = canonical({ ...store, ...JSON.parse(String(init.body)) })
    return { ok: true, json: async () => canonical(store) }
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('setMcpGate', () => {
  it('sends the partial patch to PUT /api/mcp/gate and adopts the canonical response', async () => {
    const fetchMock = stubGateFetch()
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    await act(() => setMcpGate({ enabled: true }))

    expect(fetchMock).toHaveBeenCalledWith('/api/mcp/gate', expect.objectContaining({ method: 'PUT', body: JSON.stringify({ enabled: true }) }))
    expect(result.current.enabled).toBe(true)
    expect(JSON.parse(localStorage.getItem('netsk8.mcpgate')!).enabled).toBe(true)
  })

  it('mirrors the backend enabled/allowWrite invariant: allowWrite never sticks while disabled', async () => {
    stubGateFetch({ enabled: true, allowWrite: true, readOnlyContexts: [], readDisabledContexts: [] })
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    await act(() => setMcpGate({ enabled: false }))

    expect(result.current.enabled).toBe(false)
    expect(result.current.allowWrite).toBe(false)
  })

  it('rejects and leaves local state untouched when the write fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) }))
    const { setMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    await expect(setMcpGate({ enabled: true })).rejects.toThrow(/500/)
    expect(result.current.enabled).toBe(false)
  })
})

describe('hydrateMcpGate', () => {
  it('adopts the server gate at startup (server wins over an empty localStorage)', async () => {
    stubGateFetch({ enabled: true, allowWrite: true, readOnlyContexts: ['prod'], readDisabledContexts: [] })
    const { hydrateMcpGate, useMcpGate } = await import('./mcpGate')
    const { result } = renderHook(() => useMcpGate())

    hydrateMcpGate()

    await waitFor(() => expect(result.current.enabled).toBe(true))
    expect(result.current.allowWrite).toBe(true)
    expect(result.current.readOnlyContexts).toEqual(['prod'])
  })

  it('is a no-op on a second call', async () => {
    const fetchMock = stubGateFetch()
    const { hydrateMcpGate } = await import('./mcpGate')
    hydrateMcpGate()
    hydrateMcpGate()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  })
})
