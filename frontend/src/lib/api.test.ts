import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  applyManifest,
  deleteResource,
  listPortForwards,
  restartRollout,
  rolloutHistory,
  rolloutUndo,
  scaleResource,
  startPortForward,
  stopPortForward,
} from './api'

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

describe('applyManifest', () => {
  it('does a plain PUT with no query string by default', async () => {
    const fetchMock = mockFetch(true)
    const result = await applyManifest('my-ctx', 'deployment', 'prod', 'web', 'yaml-here')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toBeUndefined()
  })

  it('adds ?dryRun=true and returns the previewed yaml when requested', async () => {
    const fetchMock = mockFetch(true, { yaml: 'previewed-yaml' })
    const result = await applyManifest('my-ctx', 'deployment', 'prod', 'web', 'yaml-here', { dryRun: true })
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web?dryRun=true', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toBe('previewed-yaml')
  })
})

describe('rollout history/undo', () => {
  it('rolloutHistory fetches the revision list', async () => {
    const fetchMock = mockFetch(true, [{ revision: 1, images: ['app:v1'], createdAt: '2026-01-01T00:00:00Z', current: true }])
    const result = await rolloutHistory('my-ctx', 'deployment', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/rollout-history/deployment/prod/web')
    expect(result).toHaveLength(1)
  })

  it('rolloutUndo sends a POST with the target revision', async () => {
    const fetchMock = mockFetch(true)
    await rolloutUndo('my-ctx', 'deployment', 'prod', 'web', 1)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/rollout-undo/deployment/prod/web', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ toRevision: 1 }),
    })
  })
})

describe('port-forward', () => {
  it('startPortForward sends a POST with the port and returns id/localPort', async () => {
    const fetchMock = mockFetch(true, { id: 'abc', localPort: 54321 })
    const result = await startPortForward('my-ctx', 'prod', 'web-1', 8080)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/portforward/prod/web-1', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ port: 8080 }),
    })
    expect(result).toEqual({ id: 'abc', localPort: 54321 })
  })

  it('stopPortForward sends a DELETE', async () => {
    const fetchMock = mockFetch(true)
    await stopPortForward('my-ctx', 'abc')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/portforward/abc', { method: 'DELETE' })
  })

  it('listPortForwards fetches active sessions', async () => {
    const fetchMock = mockFetch(true, [{ id: 'abc', namespace: 'prod', pod: 'web-1', port: 8080, localPort: 54321 }])
    const result = await listPortForwards('my-ctx')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/portforward')
    expect(result).toHaveLength(1)
  })
})
