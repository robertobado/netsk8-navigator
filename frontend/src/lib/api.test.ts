import { afterEach, describe, expect, it, vi } from 'vitest'
import { deleteResource, restartRollout, scaleResource } from './api'

function mockFetch(ok: boolean, body?: unknown, status = 200, statusText = 'OK') {
  const fn = vi.fn().mockResolvedValue({ ok, status, statusText, json: async () => body })
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('deleteResource', () => {
  it('sends a DELETE to the manifest endpoint', async () => {
    const fetchMock = mockFetch(true)
    await deleteResource('my-ctx', 'deployment', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web', { method: 'DELETE' })
  })

  it('uses "-" as the namespace segment for cluster-scoped resources', async () => {
    const fetchMock = mockFetch(true)
    await deleteResource('my-ctx', 'node', '', 'node-1')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/node/-/node-1', { method: 'DELETE' })
  })

  it('throws the backend error message on failure', async () => {
    mockFetch(false, { error: 'nope' }, 409, 'Conflict')
    await expect(deleteResource('my-ctx', 'deployment', 'prod', 'web')).rejects.toThrow('nope')
  })

  it('falls back to the status line when the error body is not JSON', async () => {
    const fn = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => {
        throw new Error('not json')
      },
    })
    vi.stubGlobal('fetch', fn)
    await expect(deleteResource('my-ctx', 'deployment', 'prod', 'web')).rejects.toThrow('500 Internal Server Error')
  })
})

describe('scaleResource', () => {
  it('sends a PUT with the replicas body', async () => {
    const fetchMock = mockFetch(true)
    await scaleResource('my-ctx', 'deployment', 'prod', 'web', 5)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/scale/deployment/prod/web', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ replicas: 5 }),
    })
  })

  it('propagates the backend error message on failure', async () => {
    mockFetch(false, { error: 'kind cannot be scaled' }, 400, 'Bad Request')
    await expect(scaleResource('my-ctx', 'service', 'prod', 'web', 3)).rejects.toThrow('kind cannot be scaled')
  })
})

describe('restartRollout', () => {
  it('sends a POST to the rollout-restart endpoint', async () => {
    const fetchMock = mockFetch(true)
    await restartRollout('my-ctx', 'deployment', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/rollout-restart/deployment/prod/web', { method: 'POST' })
  })
})
