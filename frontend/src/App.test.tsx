import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import type { ContextInfo, Issues, Overview } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

// Heavy/irrelevant subtrees (charts, canvas/WebGL, cmdk, monaco-adjacent panels,
// xyflow) get stubbed so this file can focus on AppMain's own branching —
// ctx fallback, the empty/error states, the view switch, the banners — rather
// than re-testing each child's internals (they already have their own specs).
vi.mock('@/components/VantaBackground', () => ({ VantaBackground: () => null }))
vi.mock('@/components/MetricsSection', () => ({ MetricsSection: () => <div data-testid="metrics-section-stub" /> }))
vi.mock('@/components/CommandPalette', () => ({
  CommandPalette: ({ open }: { open: boolean }) => (open ? <div data-testid="command-palette-stub" /> : null),
}))
vi.mock('@/components/PreferencesDialog', () => ({
  PreferencesDialog: ({ open }: { open: boolean }) => (open ? <div data-testid="preferences-dialog-stub" /> : null),
}))
vi.mock('@/components/ResourceDrawer', () => ({ ResourceDrawer: () => null }))
vi.mock('@/components/PodDrawer', () => ({ PodDrawer: () => null }))
vi.mock('@/components/PodsTable', () => ({ PodsTable: () => <div data-testid="pods-table-stub" /> }))
vi.mock('@/components/TopologyView', () => ({ TopologyView: () => <div data-testid="topology-stub" /> }))
vi.mock('@/components/HelmView', () => ({ HelmView: () => <div data-testid="helm-stub" /> }))
vi.mock('@/components/ResourceView', () => ({ ResourceView: () => <div data-testid="resource-view-stub" /> }))
vi.mock('@/components/CustomResourceView', () => ({ CustomResourceView: () => <div data-testid="custom-resource-stub" /> }))
vi.mock('@/components/CRDKindView', () => ({ CRDKindView: () => <div data-testid="crdkind-stub" /> }))
vi.mock('@/components/EventsView', () => ({ EventsView: () => <div data-testid="events-view-stub" /> }))
vi.mock('@/lib/useLivePods', () => ({ useLivePods: () => ({ pods: [], state: 'live' as const }) }))

// jsdom has no EventSource — App's show-about listener (App.tsx) opens one
// unconditionally on mount, so every test needs a stand-in or rendering
// throws. See lib/useLivePods.test.ts for the same pattern.
class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  onmessage: ((e: { data: string }) => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  close() {}
}

const { contextsMock, healthMock, updateCheckMock, overviewMock, namespacesMock, routeKindsMock, crdKindsMock, issuesMock } = vi.hoisted(() => ({
  contextsMock: vi.fn(),
  healthMock: vi.fn(),
  updateCheckMock: vi.fn(),
  overviewMock: vi.fn(),
  namespacesMock: vi.fn(),
  routeKindsMock: vi.fn(),
  crdKindsMock: vi.fn(),
  issuesMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      contexts: contextsMock,
      health: healthMock,
      updateCheck: updateCheckMock,
      overview: overviewMock,
      namespaces: namespacesMock,
      routeKinds: routeKindsMock,
      crdKinds: crdKindsMock,
      issues: issuesMock,
    },
  }
})

function ctxInfo(overrides: Partial<ContextInfo> = {}): ContextInfo {
  return { name: 'prod', cluster: 'prod-cluster', user: 'me', namespace: 'default', server: 'https://prod.example.com', current: true, ...overrides }
}

function overview(overrides: Partial<Overview> = {}): Overview {
  return { nodes: 3, readyNodes: 3, pods: 12, namespaces: 4, running: 10, pending: 1, failed: 1, ...overrides }
}

const emptyIssues: Issues = { pending: [], failed: [], nodesNotReady: [] }

function renderApp() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  window.location.hash = ''
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  healthMock.mockReset().mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: 'test', authEnabled: true })
  updateCheckMock.mockReset().mockResolvedValue({ available: false })
  overviewMock.mockReset().mockResolvedValue(overview())
  namespacesMock.mockReset().mockResolvedValue([])
  routeKindsMock.mockReset().mockResolvedValue([])
  crdKindsMock.mockReset().mockResolvedValue([])
  issuesMock.mockReset().mockResolvedValue(emptyIssues)
  contextsMock.mockReset()
})

afterEach(() => {
  window.location.hash = ''
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('renders the loader design preview when the hash is #loader', () => {
    window.location.hash = '#loader'
    contextsMock.mockResolvedValue([])
    renderApp()
    expect(screen.getByText(/SVG puro, escala sem perda/)).toBeInTheDocument()
  })

  it('shows the connecting empty state while contexts are still loading', () => {
    contextsMock.mockReturnValue(new Promise(() => {})) // never resolves — stays "loading"
    renderApp()
    expect(screen.getByText('app.loadingClusters')).toBeInTheDocument()
    expect(screen.queryByText('app.selectCluster')).not.toBeInTheDocument()
  })

  it('shows the select-cluster empty state once contexts resolve with no context yet', async () => {
    contextsMock.mockResolvedValue([])
    renderApp()
    expect(await screen.findByText('app.selectCluster')).toBeInTheDocument()
  })

  it('falls back to the kubeconfig current context when nothing is persisted', async () => {
    contextsMock.mockResolvedValue([ctxInfo({ name: 'staging', current: false }), ctxInfo({ name: 'prod', current: true })])
    renderApp()
    // "prod" shows up in both the ContextSwitcher button and the header
    // breadcrumb once selection settles — just confirm the fallback fired.
    await waitFor(() => expect(localStorage.getItem('netsk8s.ctx')).toBe('prod'))
    expect(screen.getAllByText('prod').length).toBeGreaterThan(0)
  })

  it('shows an ErrorBanner when the contexts request fails', async () => {
    contextsMock.mockRejectedValue(new Error('exec: no such credential helper'))
    renderApp()
    expect(await screen.findByText('Falha ao consultar o cluster')).toBeInTheDocument()
    expect(screen.getByText('exec: no such credential helper')).toBeInTheDocument()
  })

  it('renders the overview panel (stat cards) for a selected context on the default view', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    overviewMock.mockResolvedValue(overview({ nodes: 5, readyNodes: 4 }))
    renderApp()
    // "Nodes"/"Pods" also label sidebar nav entries, so assert on values that
    // are only ever rendered inside OverviewPanel's stat cards.
    expect(await screen.findByText('4 ready')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByTestId('metrics-section-stub')).toBeInTheDocument()
  })

  it('switches to the pods view when the hash is #pods', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    window.location.hash = '#pods'
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    renderApp()
    expect(await screen.findByTestId('pods-table-stub')).toBeInTheDocument()
    // OverviewPanel (and its MetricsSection) only mounts on the overview view.
    expect(screen.queryByTestId('metrics-section-stub')).not.toBeInTheDocument()
  })

  it('switches to the events view when the hash is #events', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    window.location.hash = '#events'
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    renderApp()
    expect(await screen.findByTestId('events-view-stub')).toBeInTheDocument()
  })

  it('opens the About dialog when the /api/app-events SSE stream sends show-about', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    renderApp()
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    expect(FakeEventSource.instances[0].url).toBe('/api/app-events')
    expect(screen.queryByText('about.title')).not.toBeInTheDocument()

    FakeEventSource.instances[0].onmessage?.({ data: 'show-about' })

    expect(await screen.findByText('about.title')).toBeInTheDocument()
  })

  it('shows a demo-mode banner when the backend reports demo mode', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: true, version: 'test', authEnabled: true })
    renderApp()
    expect(await screen.findByTitle('demo.banner')).toBeInTheDocument()
  })

  it('shows an update-available banner when a newer release is detected', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    updateCheckMock.mockResolvedValue({ available: true, latest: 'v1.2.3', url: 'https://example.com/releases/v1.2.3' })
    renderApp()
    const link = await screen.findByTitle('update.availablev1.2.3')
    expect(link).toHaveAttribute('href', 'https://example.com/releases/v1.2.3')
  })

  it('opens the command palette on Ctrl+K', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    renderApp()
    await screen.findByText('3 ready') // wait for the overview render to settle
    expect(screen.queryByTestId('command-palette-stub')).not.toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })
    expect(screen.getByTestId('command-palette-stub')).toBeInTheDocument()
  })

  it('opens the mobile sidebar from the header button and closes it via the backdrop', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    const user = userEvent.setup()
    renderApp()
    await screen.findByText('3 ready')

    await user.click(screen.getByLabelText('app.openMenu'))
    const backdrop = document.querySelector('[aria-hidden="true"].fixed.inset-0')
    expect(backdrop).not.toBeNull()

    await user.click(backdrop!)
    expect(document.querySelector('[aria-hidden="true"].fixed.inset-0')).toBeNull()
  })

  it('opens the preferences dialog from the header gear button', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    const user = userEvent.setup()
    renderApp()
    await screen.findByText('3 ready')

    expect(screen.queryByTestId('preferences-dialog-stub')).not.toBeInTheDocument()
    await user.click(screen.getByLabelText('Preferences'))
    expect(screen.getByTestId('preferences-dialog-stub')).toBeInTheDocument()
  })

  it('shows the running version next to the sidebar logo and opens the About dialog when clicked', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    contextsMock.mockResolvedValue([ctxInfo({ name: 'prod', current: true })])
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: '1.2.3', authEnabled: true })
    const user = userEvent.setup()
    renderApp()
    await screen.findByText('3 ready')

    const versionBadge = await screen.findByText('v1.2.3')
    // level: 3 disambiguates the About dialog's own title from the sidebar's
    // "Netsk8 Navigator" <h1>, which is also present (and also an implicit
    // "heading" role) throughout this test.
    expect(screen.queryByRole('heading', { name: 'Netsk8 Navigator', level: 3 })).not.toBeInTheDocument()

    await user.click(versionBadge)
    expect(screen.getByRole('heading', { name: 'Netsk8 Navigator', level: 3 })).toBeInTheDocument()
  })

  it('switching context via ContextSwitcher persists the new ctx and resets the namespace', async () => {
    localStorage.setItem('netsk8s.ctx', 'prod')
    localStorage.setItem('netsk8s.ns', 'kube-system')
    contextsMock.mockResolvedValue([
      ctxInfo({ name: 'prod', current: true }),
      ctxInfo({ name: 'staging', current: false, server: 'https://staging.example.com' }),
    ])
    const user = userEvent.setup()
    renderApp()
    await screen.findByText('3 ready')

    await user.click(screen.getByRole('button', { name: /prod/ }))
    await user.click(screen.getByRole('button', { name: /staging/ }))

    expect(localStorage.getItem('netsk8s.ctx')).toBe('staging')
    expect(localStorage.getItem('netsk8s.ns')).toBeNull()
  })
})
