import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ServiceAccountExpansion, WorkloadPodsExpansion, NodeExpansion, NamespaceExpansion } from './ResourceExpansions'
import type { Pod } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({
  useT: () => (key: string) => key,
  tf: (_t: unknown, key: string, vars: Record<string, string>) => Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(v), key),
}))

const { serviceAccountUsageMock, workloadPodsMock, nodeWorkloadsMock, namespaceSummaryMock } = vi.hoisted(() => ({
  serviceAccountUsageMock: vi.fn(),
  workloadPodsMock: vi.fn(),
  nodeWorkloadsMock: vi.fn(),
  namespaceSummaryMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      serviceAccountUsage: serviceAccountUsageMock,
      workloadPods: workloadPodsMock,
      nodeWorkloads: nodeWorkloadsMock,
      namespaceSummary: namespaceSummaryMock,
    },
  }
})

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

function pod(overrides: Partial<Pod> = {}): Pod {
  return {
    name: 'web-1',
    namespace: 'prod',
    status: 'Running',
    ready: 1,
    total: 1,
    restarts: 0,
    node: 'node-1',
    ip: '10.0.0.5',
    age: '',
    containers: ['app'],
    ownerKind: 'Deployment',
    ownerName: 'web',
    reason: '',
    deletedAt: '',
    finalizers: [],
    ...overrides,
  }
}

beforeEach(() => {
  serviceAccountUsageMock.mockReset()
  workloadPodsMock.mockReset()
  nodeWorkloadsMock.mockReset()
  namespaceSummaryMock.mockReset()
})

describe('ServiceAccountExpansion', () => {
  it('renders the effective permissions unioned from every bound role', async () => {
    serviceAccountUsageMock.mockResolvedValue({
      bindings: [{ kind: 'RoleBinding', slug: 'rolebinding', namespace: 'prod', name: 'web-pod-reader' }],
      pods: [],
      permissions: [
        { label: 'get,list', value: 'core/pods' },
        { label: 'get', value: 'core/nodes' },
      ],
    })
    renderWithClient(<ServiceAccountExpansion ctx="c" namespace="prod" name="web" onOpen={vi.fn()} />)

    expect(await screen.findByText('get,list')).toBeInTheDocument()
    expect(screen.getByText('core/pods')).toBeInTheDocument()
    expect(screen.getByText('get')).toBeInTheDocument()
    expect(screen.getByText('core/nodes')).toBeInTheDocument()
  })

  it('shows no permissions section when the SA has no bindings', async () => {
    serviceAccountUsageMock.mockResolvedValue({ bindings: [], pods: [], permissions: [] })
    renderWithClient(<ServiceAccountExpansion ctx="c" namespace="prod" name="orphan" onOpen={vi.fn()} />)

    expect(await screen.findByText('No binding references this SA and no pod uses it.')).toBeInTheDocument()
    expect(screen.queryByText('Effective permissions')).not.toBeInTheDocument()
  })
})

describe('WorkloadPodsExpansion', () => {
  it('shows a loading state while the pods query is pending', () => {
    workloadPodsMock.mockReturnValue(new Promise(() => {}))
    renderWithClient(<WorkloadPodsExpansion ctx="c" kind="deployment" namespace="prod" name="web" onOpen={vi.fn()} />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('lists the workload pods and opens the clicked one', async () => {
    const onOpen = vi.fn()
    workloadPodsMock.mockResolvedValue([pod({ name: 'web-1' })])
    renderWithClient(<WorkloadPodsExpansion ctx="c" kind="deployment" namespace="prod" name="web" onOpen={onOpen} />)
    fireEvent.click(await screen.findByText('web-1'))
    expect(onOpen).toHaveBeenCalledWith('pod', 'prod', 'web-1')
  })

  it('labels the list "Backend pods" for a Service instead of "Pods"', async () => {
    workloadPodsMock.mockResolvedValue([])
    renderWithClient(<WorkloadPodsExpansion ctx="c" kind="service" namespace="prod" name="web" onOpen={vi.fn()} />)
    expect(await screen.findByText("No pods match this service's selector.")).toBeInTheDocument()
  })

  it('the "View details" link opens the parent resource, not a pod', async () => {
    const onOpen = vi.fn()
    workloadPodsMock.mockResolvedValue([])
    renderWithClient(<WorkloadPodsExpansion ctx="c" kind="deployment" namespace="prod" name="web" onOpen={onOpen} />)
    fireEvent.click(await screen.findByText('View deployment details'))
    expect(onOpen).toHaveBeenCalledWith('deployment', 'prod', 'web')
  })
})

describe('NodeExpansion', () => {
  it('shows an empty state when nothing is scheduled on the node', async () => {
    nodeWorkloadsMock.mockResolvedValue([])
    renderWithClient(<NodeExpansion ctx="c" node="node-1" onOpen={vi.fn()} />)
    expect(await screen.findByText('No pods scheduled on this node.')).toBeInTheDocument()
  })

  it('groups pods by owning workload and opens either the workload or a pod', async () => {
    const onOpen = vi.fn()
    nodeWorkloadsMock.mockResolvedValue([
      { kind: 'Deployment', slug: 'deployment', namespace: 'prod', name: 'web', pods: [pod({ name: 'web-1' })] },
      { kind: 'Pod', slug: '', namespace: '', name: '', pods: [pod({ name: 'standalone-1', ownerKind: '' })] },
    ])
    renderWithClient(<NodeExpansion ctx="c" node="node-1" onOpen={onOpen} />)

    fireEvent.click(await screen.findByText('prod/web'))
    expect(onOpen).toHaveBeenCalledWith('deployment', 'prod', 'web')
    expect(screen.getByText('Standalone pods')).toBeInTheDocument()
    expect(screen.getByText('standalone-1')).toBeInTheDocument()
  })
})

describe('NamespaceExpansion', () => {
  it('shows an empty state when the namespace has no resources', async () => {
    namespaceSummaryMock.mockResolvedValue([])
    renderWithClient(<NamespaceExpansion ctx="c" ns="prod" onOpen={vi.fn()} />)
    expect(await screen.findByText('No resources in this namespace.')).toBeInTheDocument()
  })

  it('groups resources by kind and opens the clicked item', async () => {
    const onOpen = vi.fn()
    namespaceSummaryMock.mockResolvedValue([{ kind: 'ConfigMap', slug: 'configmap', items: [{ namespace: 'prod', name: 'app-config' }] }])
    renderWithClient(<NamespaceExpansion ctx="c" ns="prod" onOpen={onOpen} />)

    fireEvent.click(await screen.findByText('app-config'))
    expect(onOpen).toHaveBeenCalledWith('configmap', 'prod', 'app-config')
  })
})
