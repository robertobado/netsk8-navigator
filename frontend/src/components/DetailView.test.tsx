import type { ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { DetailBody, DetailView } from './DetailView'
import type { Pod, ResourceDetail } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { getDetailMock, workloadPodsMock } = vi.hoisted(() => ({
  getDetailMock: vi.fn(),
  workloadPodsMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, getDetail: getDetailMock, api: { ...actual.api, workloadPods: workloadPodsMock } }
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

function detail(sections: ResourceDetail['sections']): ResourceDetail {
  return {
    kind: 'Widget',
    name: 'w1',
    namespace: 'prod',
    age: '',
    ownerKind: '',
    ownerName: '',
    status: [],
    sections,
    selector: null,
    images: [],
    conditions: [],
    labels: null,
    refs: null,
    blocks: null,
    hosts: null,
    ports: null,
  }
}

describe('DetailBody — generic CRD field rendering', () => {
  it('renders a scalar field as a plain label/value row', () => {
    render(<DetailBody d={detail([{ title: 'Spec', items: [{ label: 'statusCode', value: '503' }] }])} ctx="c" kind="widget" namespace="prod" name="w1" />)
    expect(screen.getByText('statusCode')).toBeInTheDocument()
    expect(screen.getByText('503')).toBeInTheDocument()
  })

  it('renders a simple array field as chips instead of "N items"', () => {
    render(
      <DetailBody
        d={detail([{ title: 'Spec', items: [{ label: 'dnsNames', value: '', chips: ['example.com', 'www.example.com'] }] }])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('dnsNames')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByText('www.example.com')).toBeInTheDocument()
    expect(screen.queryByText(/items/)).not.toBeInTheDocument()
  })

  it('renders an object with only simple fields as a nested grid, not "{N fields}"', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [
              {
                label: 'privateKey',
                value: '',
                grid: [
                  { label: 'algorithm', value: 'ECDSA' },
                  { label: 'size', value: '256' },
                ],
              },
            ],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('privateKey')).toBeInTheDocument()
    expect(screen.getByText('algorithm')).toBeInTheDocument()
    expect(screen.getByText('ECDSA')).toBeInTheDocument()
    expect(screen.getByText('size')).toBeInTheDocument()
    expect(screen.getByText('256')).toBeInTheDocument()
    expect(screen.queryByText(/fields}/)).not.toBeInTheDocument()
  })

  it('renders a deeply-nested field as a read-only YAML code block, not "{N fields}"', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [{ label: 'directResponse', value: '', code: 'statusCode: 503\nbody:\n  inline: unavailable' }],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('directResponse')).toBeInTheDocument()
    expect(screen.getByText('yaml')).toBeInTheDocument()
    const code = screen.getByText((_, el) => el?.tagName === 'PRE' && !!el.textContent?.includes('statusCode: 503'))
    expect(code).toBeInTheDocument()
    expect(screen.queryByText(/fields}/)).not.toBeInTheDocument()
  })

  it('nested grid rows can themselves be chips (e.g. subject.organizations)', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [{ label: 'subject', value: '', grid: [{ label: 'organizations', value: '', chips: ['Netsk8 Inc'] }] }],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('organizations')).toBeInTheDocument()
    expect(screen.getByText('Netsk8 Inc')).toBeInTheDocument()
  })
})

describe('DetailBody — pod problem banner', () => {
  it('renders the reason and message when the pod has a problem', () => {
    const d = detail([])
    d.problem = { reason: 'CrashLoopBackOff', message: 'back-off restarting failed container', tone: 'err' }
    render(<DetailBody d={d} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    expect(screen.getByText('back-off restarting failed container')).toBeInTheDocument()
  })

  it('falls back to a placeholder when the reason has no message', () => {
    const d = detail([])
    d.problem = { reason: 'Unschedulable', message: '', tone: 'warn' }
    render(<DetailBody d={d} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.getByText('Unschedulable')).toBeInTheDocument()
    expect(screen.getByText('no detail')).toBeInTheDocument()
  })

  it('renders no banner for a healthy pod', () => {
    render(<DetailBody d={detail([])} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.queryByText('no detail')).not.toBeInTheDocument()
  })
})

describe('DetailView', () => {
  it('shows a loading state while the detail query is pending', () => {
    getDetailMock.mockReturnValue(new Promise(() => {}))
    renderWithClient(<DetailView ctx="c" kind="pod" namespace="prod" name="web-1" />)
    expect(screen.getByText('Loading details...')).toBeInTheDocument()
  })

  it('shows the error message when the detail query fails', async () => {
    getDetailMock.mockRejectedValue(new Error('exec: no such credential helper'))
    renderWithClient(<DetailView ctx="c" kind="pod" namespace="prod" name="web-1" />)
    expect(await screen.findByText('exec: no such credential helper')).toBeInTheDocument()
  })

  it('renders the fetched detail via DetailBody once loaded', async () => {
    getDetailMock.mockResolvedValue(detail([{ title: 'Spec', items: [{ label: 'statusCode', value: '503' }] }]))
    renderWithClient(<DetailView ctx="c" kind="pod" namespace="prod" name="web-1" />)
    expect(await screen.findByText('statusCode')).toBeInTheDocument()
    expect(getDetailMock).toHaveBeenCalledWith('c', 'pod', 'prod', 'web-1')
  })
})

describe('DetailBody — status tiles', () => {
  it('renders each status tile label and value', () => {
    const d = detail([])
    d.status = [
      { label: 'Ready', value: '2/2', tone: 'ok' },
      { label: 'Phase', value: 'Running', tone: 'muted' },
    ]
    render(<DetailBody d={d} ctx="c" kind="deployment" namespace="prod" name="web" />)
    expect(screen.getByText('Ready')).toBeInTheDocument()
    expect(screen.getByText('2/2')).toBeInTheDocument()
    expect(screen.getByText('Phase')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
  })
})

describe('DetailBody — owner link', () => {
  it('renders the owner as a clickable link when the owner kind maps to a known slug', () => {
    const d = detail([])
    d.ownerKind = 'Deployment'
    d.ownerName = 'web'
    d.namespace = 'prod'
    const onOpenResource = vi.fn()
    render(<DetailBody d={d} ctx="c" kind="replicaset" namespace="prod" name="web-abc123" onOpenResource={onOpenResource} />)
    const link = screen.getByRole('button', { name: 'Deployment/web' })
    link.click()
    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'deployment', namespace: 'prod', name: 'web' })
  })

  it('renders the owner as plain text when no onOpenResource is given', () => {
    const d = detail([])
    d.ownerKind = 'Deployment'
    d.ownerName = 'web'
    render(<DetailBody d={d} ctx="c" kind="replicaset" namespace="prod" name="web-abc123" />)
    expect(screen.queryByRole('button', { name: 'Deployment/web' })).not.toBeInTheDocument()
    expect(screen.getByText('Deployment/web')).toBeInTheDocument()
  })
})

describe('DetailBody — hosts', () => {
  it('renders a wildcard host as plain text and a concrete host as an outbound link', () => {
    const d = detail([])
    d.hosts = ['*.example.com', 'api.example.com']
    render(<DetailBody d={d} ctx="c" kind="ingress" namespace="prod" name="web" />)
    expect(screen.getByText('*.example.com')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '*.example.com' })).not.toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'api.example.com' })
    expect(link).toHaveAttribute('href', 'https://api.example.com')
    expect(link).toHaveAttribute('target', '_blank')
  })
})

describe('DetailBody — images', () => {
  it('renders each image label/value pair', () => {
    const d = detail([])
    d.images = [{ label: 'app', value: 'nginx:1.25' }]
    render(<DetailBody d={d} ctx="c" kind="configmap" namespace="prod" name="web-1" />)
    expect(screen.getByText('app')).toBeInTheDocument()
    expect(screen.getByText('nginx:1.25')).toBeInTheDocument()
  })
})

describe('DetailBody — ports', () => {
  it('renders a port pill with name, port, protocol and extra', () => {
    const d = detail([])
    d.ports = [{ name: 'http', port: '80', protocol: 'TCP', extra: '→ 8080' }]
    render(<DetailBody d={d} ctx="c" kind="service" namespace="prod" name="web" />)
    expect(screen.getByText('http')).toBeInTheDocument()
    expect(screen.getByText('80')).toBeInTheDocument()
    expect(screen.getByText('TCP')).toBeInTheDocument()
    expect(screen.getByText('→ 8080')).toBeInTheDocument()
  })
})

describe('DetailBody — cross-link refs', () => {
  it('groups refs by group into cards and opens the clicked one', () => {
    const d = detail([])
    d.refs = [{ group: 'Backends', kind: 'service', namespace: 'prod', name: 'backend', note: 'not ready · demo' }]
    const onOpenResource = vi.fn()
    render(<DetailBody d={d} ctx="c" kind="ingress" namespace="prod" name="web" onOpenResource={onOpenResource} />)
    expect(screen.getByText('Backends')).toBeInTheDocument()
    expect(screen.getByText('backend')).toBeInTheDocument()
    expect(screen.getByText('not ready · demo')).toBeInTheDocument()
    screen.getByText('backend').closest('button')!.click()
    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'service', namespace: 'prod', name: 'backend' })
  })

  it('renders no cross-link cards when onOpenResource is not given', () => {
    const d = detail([])
    d.refs = [{ group: 'Backends', kind: 'service', namespace: 'prod', name: 'backend' }]
    render(<DetailBody d={d} ctx="c" kind="ingress" namespace="prod" name="web" />)
    expect(screen.queryByText('Backends')).not.toBeInTheDocument()
  })
})

describe('DetailBody — content blocks (DataRow)', () => {
  it('renders a short scalar block inline', () => {
    const d = detail([])
    d.blocks = [{ title: 'PORT', body: '8080' }]
    render(<DetailBody d={d} ctx="c" kind="configmap" namespace="prod" name="cfg" />)
    expect(screen.getByText('PORT')).toBeInTheDocument()
    expect(screen.getByText('8080')).toBeInTheDocument()
  })

  it('expands and collapses a multiline block on click', () => {
    const d = detail([])
    d.blocks = [{ title: 'nginx.conf', body: 'line one\nline two\nline three' }]
    render(<DetailBody d={d} ctx="c" kind="configmap" namespace="prod" name="cfg" />)
    expect(screen.getByText('line one')).toBeInTheDocument()
    expect(screen.queryByText(/line two/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByText('nginx.conf').closest('button')!)
    expect(screen.getByText(/line two/)).toBeInTheDocument()
    fireEvent.click(screen.getByText('nginx.conf').closest('button')!)
    expect(screen.queryByText(/line two/)).not.toBeInTheDocument()
  })

  it('keeps a masked block hidden until revealed, then re-hides it', () => {
    const d = detail([])
    d.blocks = [{ title: 'API_KEY', body: 'super-secret', masked: true }]
    render(<DetailBody d={d} ctx="c" kind="secret" namespace="prod" name="creds" />)
    expect(screen.queryByText('super-secret')).not.toBeInTheDocument()
    fireEvent.click(screen.getByTitle('Reveal value'))
    expect(screen.getByText('super-secret')).toBeInTheDocument()
    fireEvent.click(screen.getByTitle('Hide value'))
    expect(screen.queryByText('super-secret')).not.toBeInTheDocument()
  })
})

describe('DetailBody — conditions, selector, labels', () => {
  it('renders condition pills with label and value', () => {
    const d = detail([])
    d.conditions = [{ label: 'Available', value: 'True', tone: 'ok' }]
    render(<DetailBody d={d} ctx="c" kind="deployment" namespace="prod" name="web" />)
    expect(screen.getByText('Available')).toBeInTheDocument()
    expect(screen.getByText('True')).toBeInTheDocument()
  })

  it('renders selector entries as key=value chips', () => {
    const d = detail([])
    d.selector = { app: 'web' }
    render(<DetailBody d={d} ctx="c" kind="service" namespace="prod" name="web" />)
    expect(screen.getByText('app=web')).toBeInTheDocument()
  })

  it('renders label entries as key=value chips, or bare key when the value is empty', () => {
    const d = detail([])
    d.labels = { team: 'platform', 'feature-flag': '' }
    render(<DetailBody d={d} ctx="c" kind="configmap" namespace="prod" name="web-1" />)
    expect(screen.getByText('team=platform')).toBeInTheDocument()
    expect(screen.getByText('feature-flag')).toBeInTheDocument()
  })
})

describe('DetailBody — hosts', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders a wildcard host as plain text, not a link', () => {
    const d = detail([])
    d.hosts = ['*.example.com']
    render(<DetailBody d={d} ctx="c" kind="ingress" namespace="prod" name="web" />)
    expect(screen.getByText('*.example.com')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  // target="_blank" alone does nothing in the Wails desktop app (see
  // openExternal's doc comment in lib/utils.ts) — the click must go through
  // window.runtime.BrowserOpenURL when that bridge is present.
  it('opens a non-wildcard host via window.runtime.BrowserOpenURL when the Wails bridge is present', () => {
    const browserOpenURL = vi.fn()
    vi.stubGlobal('runtime', { BrowserOpenURL: browserOpenURL })
    const d = detail([])
    d.hosts = ['app.example.com']
    render(<DetailBody d={d} ctx="c" kind="ingress" namespace="prod" name="web" />)

    fireEvent.click(screen.getByRole('link', { name: /app.example.com/ }))

    expect(browserOpenURL).toHaveBeenCalledWith('https://app.example.com')
  })
})

describe('DetailBody — backing pods list (WorkloadPods)', () => {
  it('shows a loading state while the workload-pods query is pending', () => {
    workloadPodsMock.mockReturnValue(new Promise(() => {}))
    renderWithClient(<DetailBody d={detail([])} ctx="c" kind="deployment" namespace="prod" name="web" onOpenPod={vi.fn()} />)
    expect(screen.getByText('Loading pods...')).toBeInTheDocument()
  })

  it('shows an empty state when the workload has no pods', async () => {
    workloadPodsMock.mockResolvedValue([])
    renderWithClient(<DetailBody d={detail([])} ctx="c" kind="deployment" namespace="prod" name="web" onOpenPod={vi.fn()} />)
    expect(await screen.findByText('No pods.')).toBeInTheDocument()
  })

  it('lists the workload pods and opens the clicked one', async () => {
    const onOpenPod = vi.fn()
    workloadPodsMock.mockResolvedValue([pod({ name: 'web-1' }), pod({ name: 'web-2' })])
    renderWithClient(<DetailBody d={detail([])} ctx="c" kind="deployment" namespace="prod" name="web" onOpenPod={onOpenPod} />)
    const row = await screen.findByText('web-1')
    row.click()
    expect(onOpenPod).toHaveBeenCalledWith(expect.objectContaining({ name: 'web-1' }))
    expect(screen.getByText('web-2')).toBeInTheDocument()
  })

  it('labels the list "Endpoints" for a Service instead of "Pods"', async () => {
    workloadPodsMock.mockResolvedValue([])
    renderWithClient(<DetailBody d={detail([])} ctx="c" kind="service" namespace="prod" name="web" onOpenPod={vi.fn()} />)
    expect(await screen.findByText('Endpoints (0)')).toBeInTheDocument()
  })

  it('does not render the pods list for a non-workload kind', () => {
    workloadPodsMock.mockClear()
    renderWithClient(<DetailBody d={detail([])} ctx="c" kind="configmap" namespace="prod" name="web" onOpenPod={vi.fn()} />)
    expect(screen.queryByText('Loading pods...')).not.toBeInTheDocument()
    expect(workloadPodsMock).not.toHaveBeenCalled()
  })
})
