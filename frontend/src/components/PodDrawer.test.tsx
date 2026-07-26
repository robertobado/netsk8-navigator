import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PodDrawer } from './PodDrawer'
import type { Pod } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('./DetailView', () => ({ DetailView: () => <div>detail-view</div> }))
vi.mock('./EventsPanel', () => ({ EventsPanel: () => null }))
vi.mock('./LogsPanel', () => ({ LogsPanel: () => null }))
vi.mock('./TerminalPanel', () => ({ TerminalPanel: () => <div>terminal-panel</div> }))
vi.mock('./PortForwardPanel', () => ({ PortForwardPanel: () => <div>forward-panel</div> }))
vi.mock('./ManifestPanelLazy', () => ({ ManifestPanelLazy: () => null }))
vi.mock('./ResourceActions', () => ({ ResourceActions: () => null }))

const { healthMock } = vi.hoisted(() => ({ healthMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, health: healthMock } }
})

const pod: Pod = {
  name: 'web-1',
  namespace: 'default',
  status: 'Running',
  ready: 1,
  total: 1,
  restarts: 0,
  node: 'node-1',
  ip: '10.0.0.1',
  age: '1h',
  containers: ['web'],
  ownerKind: 'ReplicaSet',
  ownerName: 'web-1234',
  reason: '',
  deletedAt: '',
  finalizers: [],
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('PodDrawer demo mode', () => {
  it('shows the Terminal and Forward tabs when not in demo mode', async () => {
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false })
    renderWithClient(<PodDrawer pod={pod} ctx="c" onClose={() => {}} />)
    expect(await screen.findByText('Terminal')).toBeInTheDocument()
    expect(screen.getByText('Forward')).toBeInTheDocument()
  })

  it('hides the Terminal and Forward tabs in demo mode', async () => {
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: true })
    renderWithClient(<PodDrawer pod={pod} ctx="c" onClose={() => {}} />)
    expect(await screen.findByText('detail-view')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('Terminal')).not.toBeInTheDocument())
    expect(screen.queryByText('Forward')).not.toBeInTheDocument()
  })
})
