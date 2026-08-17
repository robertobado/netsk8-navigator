import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  addHelmRepo,
  applyManifest,
  applyManifestRef,
  blankManifestYAML,
  cordonNode,
  createResource,
  crdApply,
  crdDelete,
  crdDetail,
  deleteResource,
  deleteResourceRef,
  execURL,
  getManifestRef,
  helmChartDetail,
  helmRepos,
  helmReleaseHistory,
  helmReleaseManifest,
  helmReleaseRollback,
  helmReleases,
  helmReleaseStatus,
  helmReleaseUninstall,
  helmSearch,
  installHelmRelease,
  listPortForwards,
  logsURL,
  refreshHelmRepo,
  regenerateMCPToken,
  removeHelmRepo,
  restartRollout,
  rolloutHistory,
  rolloutUndo,
  scaleResource,
  startPortForward,
  stopPortForward,
  upgradeHelmRelease,
  workloadLogsURL,
  type CRDRef,
  type HelmInstallRequest,
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

describe('crdApply', () => {
  const rk: CRDRef = { group: 'example.com', version: 'v1', resource: 'widgets' }

  it('does a plain PUT with no query string by default', async () => {
    const fetchMock = mockFetch(true)
    const result = await crdApply('my-ctx', rk, 'prod', 'w1', 'yaml-here')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toBeUndefined()
  })

  it('uses "-" as the namespace segment for cluster-scoped resources', async () => {
    const fetchMock = mockFetch(true)
    await crdApply('my-ctx', { group: 'kwok.x-k8s.io', version: 'v1alpha1', resource: 'clusterresourceusages' }, '', 'usage-1', 'yaml-here')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/contexts/my-ctx/crd/kwok.x-k8s.io/v1alpha1/clusterresourceusages/-/usage-1',
      expect.objectContaining({ method: 'PUT' }),
    )
  })

  it('adds ?dryRun=true and returns the previewed yaml when requested', async () => {
    const fetchMock = mockFetch(true, { yaml: 'previewed-yaml' })
    const result = await crdApply('my-ctx', rk, 'prod', 'w1', 'yaml-here', { dryRun: true })
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1?dryRun=true', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toBe('previewed-yaml')
  })
})

describe('crdDelete', () => {
  it('sends a DELETE to the generic crd endpoint', async () => {
    const fetchMock = mockFetch(true)
    await crdDelete('my-ctx', { group: 'example.com', version: 'v1', resource: 'widgets' }, 'prod', 'w1')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1', { method: 'DELETE' })
  })
})

describe('ResourceRef dispatch helpers', () => {
  const rk: CRDRef = { group: 'example.com', version: 'v1', resource: 'widgets' }

  it('getManifestRef: a string kind hits the manifest endpoint, a CRDRef hits the generic crd endpoint', async () => {
    const fetchMock = mockFetch(true, { yaml: 'y' })
    await getManifestRef('my-ctx', 'deployment', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web')

    await getManifestRef('my-ctx', rk, 'prod', 'w1')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1/manifest')
  })

  it('applyManifestRef dispatches the same way', async () => {
    const fetchMock = mockFetch(true)
    await applyManifestRef('my-ctx', 'deployment', 'prod', 'web', 'yaml')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web', expect.objectContaining({ method: 'PUT' }))

    await applyManifestRef('my-ctx', rk, 'prod', 'w1', 'yaml')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1', expect.objectContaining({ method: 'PUT' }))
  })

  it('deleteResourceRef dispatches the same way', async () => {
    const fetchMock = mockFetch(true)
    await deleteResourceRef('my-ctx', 'deployment', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/manifest/deployment/prod/web', { method: 'DELETE' })

    await deleteResourceRef('my-ctx', rk, 'prod', 'w1')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/prod/w1', { method: 'DELETE' })
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

describe('cordonNode', () => {
  it('sends a POST with the cordon flag', async () => {
    const fetchMock = mockFetch(true)
    await cordonNode('my-ctx', 'node-1', true)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/cordon/node-1', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cordon: true }),
    })
  })

  it('propagates the backend error message on failure', async () => {
    mockFetch(false, { error: 'node not found' }, 502, 'Bad Gateway')
    await expect(cordonNode('my-ctx', 'missing', true)).rejects.toThrow('node not found')
  })
})

describe('createResource', () => {
  it('does a plain POST with no query string by default', async () => {
    const fetchMock = mockFetch(true, { status: 'created', kind: 'ConfigMap', namespace: 'prod', name: 'cfg' })
    const result = await createResource('my-ctx', 'yaml-here')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toEqual({ status: 'created', kind: 'ConfigMap', namespace: 'prod', name: 'cfg' })
  })

  it('adds ?dryRun=true and returns the previewed yaml when requested', async () => {
    const fetchMock = mockFetch(true, { yaml: 'previewed-yaml' })
    const result = await createResource('my-ctx', 'yaml-here', { dryRun: true })
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/create?dryRun=true', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml: 'yaml-here' }),
    })
    expect(result).toEqual({ yaml: 'previewed-yaml' })
  })

  it('propagates the backend error message on failure', async () => {
    mockFetch(false, { error: 'metadata.name is required' }, 400, 'Bad Request')
    await expect(createResource('my-ctx', 'yaml-here')).rejects.toThrow('metadata.name is required')
  })
})

describe('blankManifestYAML', () => {
  it('includes a namespace field for namespaced kinds', () => {
    const yaml = blankManifestYAML('deployment', 'prod', false)
    expect(yaml).toContain('apiVersion: apps/v1')
    expect(yaml).toContain('kind: Deployment')
    expect(yaml).toContain('namespace: prod')
  })

  it('defaults the namespace to "default" when none is selected', () => {
    const yaml = blankManifestYAML('configmap', '', false)
    expect(yaml).toContain('namespace: default')
  })

  it('omits the namespace field for cluster-scoped kinds', () => {
    const yaml = blankManifestYAML('namespace', '', true)
    expect(yaml).not.toContain('namespace:')
    expect(yaml).toContain('kind: Namespace')
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

describe('crdDetail', () => {
  it('fetches the detail endpoint for a CRD instance, defaulting namespace to "-"', async () => {
    const rk: CRDRef = { group: 'example.com', version: 'v1', resource: 'widgets' }
    const fetchMock = mockFetch(true, { kind: 'Widget' })
    await crdDetail('my-ctx', rk, '', 'w1')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/crd/example.com/v1/widgets/-/w1/detail')
  })
})

describe('regenerateMCPToken', () => {
  it('POSTs to the regenerate endpoint and returns the new token', async () => {
    const fetchMock = mockFetch(true, { token: 'new-token' })
    const result = await regenerateMCPToken()
    expect(fetchMock).toHaveBeenCalledWith('/api/mcp/token/regenerate', { method: 'POST' })
    expect(result).toEqual({ token: 'new-token' })
  })
})

describe('stream URL builders', () => {
  it('logsURL includes the container query param only when given one', () => {
    expect(logsURL('my-ctx', 'prod', 'web-1')).toBe('/api/contexts/my-ctx/pods/prod/web-1/logs')
    expect(logsURL('my-ctx', 'prod', 'web-1', 'app')).toBe('/api/contexts/my-ctx/pods/prod/web-1/logs?container=app')
  })

  it('workloadLogsURL builds the aggregated per-workload logs endpoint', () => {
    expect(workloadLogsURL('my-ctx', 'deployment', 'prod', 'web')).toBe('/api/contexts/my-ctx/pods-of/deployment/prod/web/logs')
    expect(workloadLogsURL('my-ctx', 'deployment', 'prod', 'web', 'app')).toBe('/api/contexts/my-ctx/pods-of/deployment/prod/web/logs?container=app')
  })

  it('execURL picks ws:// on http and wss:// on https, against the current host', () => {
    expect(execURL('my-ctx', 'prod', 'web-1')).toBe('ws://localhost/api/contexts/my-ctx/pods/prod/web-1/exec')
    expect(execURL('my-ctx', 'prod', 'web-1', 'app')).toBe('ws://localhost/api/contexts/my-ctx/pods/prod/web-1/exec?container=app')
  })
})

describe('helm', () => {
  it('helmReleases lists releases, optionally scoped to a namespace', async () => {
    const fetchMock = mockFetch(true, [])
    await helmReleases('my-ctx')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases')
    await helmReleases('my-ctx', 'prod')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases?namespace=prod')
  })

  it('helmReleaseStatus fetches one release', async () => {
    const fetchMock = mockFetch(true, {})
    await helmReleaseStatus('my-ctx', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases/prod/web')
  })

  it('helmReleaseManifest fetches and unwraps the yaml field', async () => {
    mockFetch(true, { yaml: 'kind: Deployment' })
    expect(await helmReleaseManifest('my-ctx', 'prod', 'web')).toBe('kind: Deployment')
  })

  it('helmReleaseHistory fetches revision history', async () => {
    const fetchMock = mockFetch(true, [])
    await helmReleaseHistory('my-ctx', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases/prod/web/history')
  })

  it('helmReleaseRollback POSTs the target revision', async () => {
    const fetchMock = mockFetch(true)
    await helmReleaseRollback('my-ctx', 'prod', 'web', 2)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases/prod/web/rollback', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ revision: 2 }),
    })
  })

  it('helmReleaseUninstall sends a DELETE', async () => {
    const fetchMock = mockFetch(true)
    await helmReleaseUninstall('my-ctx', 'prod', 'web')
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases/prod/web', { method: 'DELETE' })
  })

  const installReq: HelmInstallRequest = { repo: 'bitnami', chart: 'nginx', version: '1.2.3', releaseName: 'web', namespace: 'prod', values: '' }

  it('installHelmRelease POSTs the install request', async () => {
    const fetchMock = mockFetch(true, { name: 'web' })
    const result = await installHelmRelease('my-ctx', installReq)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(installReq),
    })
    expect(result).toEqual({ name: 'web' })
  })

  it('upgradeHelmRelease PUTs the upgrade request', async () => {
    const fetchMock = mockFetch(true, { name: 'web' })
    await upgradeHelmRelease('my-ctx', 'prod', 'web', installReq)
    expect(fetchMock).toHaveBeenCalledWith('/api/contexts/my-ctx/helm/releases/prod/web', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(installReq),
    })
  })

  it('helmRepos lists locally-added repos', async () => {
    const fetchMock = mockFetch(true, [])
    await helmRepos()
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/repos')
  })

  it('addHelmRepo POSTs name+url', async () => {
    const fetchMock = mockFetch(true, { name: 'bitnami', url: 'https://charts.bitnami.com' })
    await addHelmRepo('bitnami', 'https://charts.bitnami.com')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/repos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'bitnami', url: 'https://charts.bitnami.com' }),
    })
  })

  it('removeHelmRepo sends a DELETE', async () => {
    const fetchMock = mockFetch(true)
    await removeHelmRepo('bitnami')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/repos/bitnami', { method: 'DELETE' })
  })

  it('refreshHelmRepo POSTs to the refresh endpoint', async () => {
    const fetchMock = mockFetch(true)
    await refreshHelmRepo('bitnami')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/repos/bitnami/refresh', { method: 'POST' })
  })

  it('helmSearch omits the query param when q is empty', async () => {
    const fetchMock = mockFetch(true, [])
    await helmSearch('')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/search')
    await helmSearch('nginx')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/search?q=nginx')
  })

  it('helmChartDetail includes version only when given one', async () => {
    const fetchMock = mockFetch(true, { versions: [], defaultValues: '', readme: '' })
    await helmChartDetail('bitnami', 'nginx')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/charts/bitnami/nginx')
    await helmChartDetail('bitnami', 'nginx', '1.2.3')
    expect(fetchMock).toHaveBeenCalledWith('/api/helm/charts/bitnami/nginx?version=1.2.3')
  })
})
