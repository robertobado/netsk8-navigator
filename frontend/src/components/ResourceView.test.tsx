import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import { Box } from 'lucide-react'
import { ResourceView } from './ResourceView'
import type { CreatedResource } from '@/lib/api'
import type { ResourceDef, ResourceExpand, ResourceHistory } from '@/lib/resources'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { listMock, deploymentsUsageMock } = vi.hoisted(() => ({ listMock: vi.fn(), deploymentsUsageMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, list: listMock, deploymentsUsage: deploymentsUsageMock } }
})

const { useMetricsRefreshMock } = vi.hoisted(() => ({ useMetricsRefreshMock: vi.fn() }))
vi.mock('@/lib/metrics', () => ({ useMetricsRefresh: useMetricsRefreshMock }))

const { drawerTargetSpy } = vi.hoisted(() => ({ drawerTargetSpy: vi.fn() }))
vi.mock('./ResourceDrawer', () => ({
  ResourceDrawer: (props: { target: { kind: string; namespace: string; name: string } | null }) => {
    drawerTargetSpy(props.target)
    return props.target ? <div data-testid="drawer-open">{`${props.target.kind}/${props.target.namespace}/${props.target.name}`}</div> : null
  },
}))

const { createDialogPropsSpy } = vi.hoisted(() => ({ createDialogPropsSpy: vi.fn() }))
vi.mock('./CreateResourceDialogLazy', () => ({
  CreateResourceDialogLazy: (props: { open: boolean; kind: string; namespace: string; onCreated: (r: CreatedResource) => void }) => {
    createDialogPropsSpy(props)
    if (!props.open) return null
    return (
      <button type="button" onClick={() => props.onCreated({ kind: 'ConfigMap', namespace: 'prod', name: 'new-cm' })}>
        confirm-create
      </button>
    )
  },
}))

vi.mock('./ResourceExpansions', () => ({
  NodeExpansion: (props: { node: string; onOpen: (kind: string, ns: string, name: string) => void }) => (
    <button type="button" onClick={() => props.onOpen('pod', '', 'from-node')}>{`node-expansion:${props.node}`}</button>
  ),
  NamespaceExpansion: (props: { ns: string; onOpen: (kind: string, ns: string, name: string) => void }) => (
    <button type="button" onClick={() => props.onOpen('pod', props.ns, 'from-ns')}>{`namespace-expansion:${props.ns}`}</button>
  ),
  ServiceAccountExpansion: (props: { name: string; onOpen: (kind: string, ns: string, name: string) => void }) => (
    <button type="button" onClick={() => props.onOpen('pod', 'prod', 'from-sa')}>{`sa-expansion:${props.name}`}</button>
  ),
  ConsumersExpansion: (props: { name: string; onOpen: (kind: string, ns: string, name: string) => void }) => (
    <button type="button" onClick={() => props.onOpen('pod', 'prod', 'from-consumers')}>{`consumers-expansion:${props.name}`}</button>
  ),
  WorkloadPodsExpansion: (props: { name: string; onOpen: (kind: string, ns: string, name: string) => void }) => (
    <button type="button" onClick={() => props.onOpen('pod', 'prod', 'from-workload')}>{`workload-expansion:${props.name}`}</button>
  ),
}))

interface Row {
  name: string
  namespace: string
  status: string
}
const col = createColumnHelper<Row>()
const baseColumns = [
  col.accessor('name', { header: 'Name' }),
  col.accessor('namespace', { header: 'Namespace' }),
  col.accessor('status', { header: 'Status' }),
] as ColumnDef<never, unknown>[]

function makeDef(overrides: Partial<ResourceDef> = {}): ResourceDef {
  return {
    key: 'widgets',
    label: 'Widgets',
    icon: Box,
    group: 'Config',
    resource: 'widgets',
    manifest: 'pod',
    facets: [],
    columns: baseColumns,
    ...overrides,
  }
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  listMock.mockReset()
  deploymentsUsageMock.mockReset()
  useMetricsRefreshMock.mockReset()
  useMetricsRefreshMock.mockReturnValue({ ms: 15_000, interval: 15_000 })
  drawerTargetSpy.mockClear()
  createDialogPropsSpy.mockClear()
  localStorage.clear()
})

describe('ResourceView', () => {
  it('shows a loading state while the list is being fetched', () => {
    listMock.mockReturnValue(new Promise(() => {}))
    renderWithClient(<ResourceView def={makeDef()} ctx="c" ns="" />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders rows once the list resolves', async () => {
    listMock.mockResolvedValue([{ name: 'widget-a', namespace: 'prod', status: 'Ready' }])
    renderWithClient(<ResourceView def={makeDef()} ctx="c" ns="" />)
    expect(await screen.findByText('widget-a')).toBeInTheDocument()
    expect(listMock).toHaveBeenCalledWith('c', 'widgets', undefined)
  })

  it('passes the namespace through to the list call when set', async () => {
    listMock.mockResolvedValue([])
    renderWithClient(<ResourceView def={makeDef()} ctx="c" ns="prod" />)
    await screen.findByText('No items to display.')
    expect(listMock).toHaveBeenCalledWith('c', 'widgets', 'prod')
  })

  it('shows a generic error message and lets the user retry', async () => {
    listMock.mockRejectedValue(new Error('boom: connection refused'))
    renderWithClient(<ResourceView def={makeDef()} ctx="c" ns="" />)

    expect(await screen.findByText(/Could not load/)).toBeInTheDocument()
    expect(screen.getByText('The Kubernetes API did not respond to this listing.')).toBeInTheDocument()

    listMock.mockClear()
    listMock.mockResolvedValue([])
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Try again/ }))
    expect(listMock).toHaveBeenCalled()
  })

  it('shows a credential-specific error message for auth failures', async () => {
    listMock.mockRejectedValue(new Error('exec: getting credential: token expired'))
    renderWithClient(<ResourceView def={makeDef()} ctx="c" ns="" />)
    expect(
      await screen.findByText(
        'The connection to the Kubernetes API failed — expired credential or no permission. Renew the cluster login (e.g. AWS credentials) and try again.',
      ),
    ).toBeInTheDocument()
  })

  it('opens the drawer with the resource kind on row click', async () => {
    listMock.mockResolvedValue([{ name: 'widget-a', namespace: 'prod', status: 'Ready' }])
    const user = userEvent.setup()
    renderWithClient(<ResourceView def={makeDef({ manifest: 'configmap' })} ctx="c" ns="" />)

    await user.click(await screen.findByText('widget-a'))
    expect(await screen.findByTestId('drawer-open')).toHaveTextContent('configmap/prod/widget-a')
  })

  it('shows a New button and opens the create dialog for a creatable kind', async () => {
    listMock.mockResolvedValue([])
    const user = userEvent.setup()
    renderWithClient(<ResourceView def={makeDef({ manifest: 'configmap' })} ctx="c" ns="prod" />)
    await screen.findByText('No items to display.')

    const newBtn = screen.getByRole('button', { name: /New/ })
    await user.click(newBtn)
    expect(createDialogPropsSpy).toHaveBeenLastCalledWith(expect.objectContaining({ open: true, kind: 'configmap', namespace: 'prod' }))
  })

  it('does not show a New button for a non-creatable kind', async () => {
    listMock.mockResolvedValue([])
    renderWithClient(<ResourceView def={makeDef({ manifest: 'pod' })} ctx="c" ns="" />)
    await screen.findByText('No items to display.')
    expect(screen.queryByRole('button', { name: /New/ })).not.toBeInTheDocument()
  })

  it('refetches and opens the newly created resource after onCreated', async () => {
    listMock.mockResolvedValue([])
    const user = userEvent.setup()
    renderWithClient(<ResourceView def={makeDef({ manifest: 'configmap' })} ctx="c" ns="prod" />)
    await screen.findByText('No items to display.')

    listMock.mockClear()
    await user.click(screen.getByRole('button', { name: /New/ }))
    await user.click(screen.getByRole('button', { name: 'confirm-create' }))

    expect(listMock).toHaveBeenCalled()
    expect(await screen.findByTestId('drawer-open')).toHaveTextContent('configmap/prod/new-cm')
    expect(screen.queryByRole('button', { name: 'confirm-create' })).not.toBeInTheDocument()
  })

  describe('usage columns', () => {
    it('adds sortable CPU/Mem gauge columns when usage data is available', async () => {
      listMock.mockResolvedValue([{ name: 'dep-a', namespace: 'prod', status: 'Ready' }])
      deploymentsUsageMock.mockResolvedValue({
        available: true,
        items: { 'prod/dep-a': { cpu: { used: 0.5, total: 1, unit: 'cores' }, memory: { used: 100, total: 200, unit: 'bytes' } } },
      })
      renderWithClient(<ResourceView def={makeDef({ usage: true })} ctx="c" ns="" />)

      expect(await screen.findByText('CPU')).toBeInTheDocument()
      expect(screen.getByText('Mem')).toBeInTheDocument()
      expect(screen.getAllByText('50%')).toHaveLength(2)

      const user = userEvent.setup()
      const cpuHeader = screen.getByText('CPU').closest('th') as HTMLElement
      await user.click(within(cpuHeader).getByTitle('Sort by absolute value'))
      // Toggling basis persists the choice for next mount; no visible re-render is
      // asserted here since the gauge always shows the same value regardless of basis.
      expect(localStorage.getItem('netsk8.usage.dep-cpu')).toBe('abs')
    })

    it('omits usage columns when metrics refresh is off', async () => {
      useMetricsRefreshMock.mockReturnValue({ ms: 0, interval: null })
      listMock.mockResolvedValue([{ name: 'dep-a', namespace: 'prod', status: 'Ready' }])
      renderWithClient(<ResourceView def={makeDef({ usage: true })} ctx="c" ns="" />)
      await screen.findByText('dep-a')
      expect(screen.queryByText('CPU')).not.toBeInTheDocument()
      expect(deploymentsUsageMock).not.toHaveBeenCalled()
    })

    it('omits usage columns when the metrics backend reports unavailable', async () => {
      listMock.mockResolvedValue([{ name: 'dep-a', namespace: 'prod', status: 'Ready' }])
      deploymentsUsageMock.mockResolvedValue({ available: false })
      renderWithClient(<ResourceView def={makeDef({ usage: true })} ctx="c" ns="" />)
      await screen.findByText('dep-a')
      expect(screen.queryByText('CPU')).not.toBeInTheDocument()
    })

    it('renders a placeholder for rows missing a usage entry', async () => {
      listMock.mockResolvedValue([{ name: 'dep-a', namespace: 'prod', status: 'Ready' }])
      deploymentsUsageMock.mockResolvedValue({ available: true, items: {} })
      renderWithClient(<ResourceView def={makeDef({ usage: true })} ctx="c" ns="" />)
      await screen.findByText('CPU')
      expect(screen.getAllByText('—').length).toBeGreaterThan(0)
    })
  })

  describe('parent -> children expand', () => {
    const expand: ResourceExpand = {
      resource: 'children',
      manifest: 'pod',
      countHeader: 'Kids',
      title: 'Children of this widget',
      parentKey: (p) => p.name as string,
      childKey: (c) => c.parent as string,
      renderChild: (c) => <span>{c.name as string}</span>,
    }

    it('shows a count column and expands to list matching children', async () => {
      listMock.mockImplementation((_ctx: string, resource: string) => {
        if (resource === 'children') return Promise.resolve([{ name: 'child-a', parent: 'widget-a', namespace: '' }])
        return Promise.resolve([{ name: 'widget-a', namespace: 'prod', status: 'Ready' }])
      })
      const user = userEvent.setup()
      renderWithClient(<ResourceView def={makeDef({ expand })} ctx="c" ns="" />)

      await screen.findByText('widget-a')
      expect(screen.getByText('Kids')).toBeInTheDocument()
      const dataRow = screen.getAllByRole('row')[1]
      expect(within(dataRow).getByText('1')).toBeInTheDocument()

      await user.click(screen.getByText('widget-a'))
      expect(await screen.findByText('Children of this widget')).toBeInTheDocument()
      expect(screen.getByText('child-a')).toBeInTheDocument()

      await user.click(screen.getByText('child-a'))
      expect(await screen.findByTestId('drawer-open')).toHaveTextContent('pod//child-a')
    })

    it('does not expand a row with no matching children', async () => {
      listMock.mockImplementation((_ctx: string, resource: string) => {
        if (resource === 'children') return Promise.resolve([])
        return Promise.resolve([{ name: 'widget-a', namespace: 'prod', status: 'Ready' }])
      })
      const user = userEvent.setup()
      renderWithClient(<ResourceView def={makeDef({ expand })} ctx="c" ns="" />)
      await screen.findByText('widget-a')
      await user.click(screen.getByText('widget-a'))
      expect(screen.queryByText('Children of this widget')).not.toBeInTheDocument()
    })
  })

  describe('revision history', () => {
    const history: ResourceHistory = {
      isOld: (r) => !r.current,
      groupKey: (r) => r.group as string,
      revision: (r) => Number(r.revision) || 0,
    }

    it('shows only the current revision, and expands to reveal older ones', async () => {
      listMock.mockResolvedValue([
        { name: 'rs-v2', namespace: 'prod', status: 'Ready', group: 'app', revision: '2', ready: '1/1', age: new Date().toISOString(), current: true },
        { name: 'rs-v1', namespace: 'prod', status: 'Old', group: 'app', revision: '1', ready: '0/1', age: new Date().toISOString(), current: false },
      ])
      const user = userEvent.setup()
      renderWithClient(<ResourceView def={makeDef({ history })} ctx="c" ns="" />)

      await screen.findByText('rs-v2')
      expect(screen.queryByText('rs-v1')).not.toBeInTheDocument()

      await user.click(screen.getByText('rs-v2'))
      expect(await screen.findByText('Revision history')).toBeInTheDocument()
      expect(screen.getByText('rs-v1')).toBeInTheDocument()

      await user.click(screen.getByText('rs-v1'))
      expect(await screen.findByTestId('drawer-open')).toHaveTextContent('pod/prod/rs-v1')
    })

    it('does not expand a revision that has no older history', async () => {
      listMock.mockResolvedValue([
        { name: 'rs-v1', namespace: 'prod', status: 'Ready', group: 'app', revision: '1', ready: '1/1', age: new Date().toISOString(), current: true },
      ])
      const user = userEvent.setup()
      renderWithClient(<ResourceView def={makeDef({ history })} ctx="c" ns="" />)
      await screen.findByText('rs-v1')
      await user.click(screen.getByText('rs-v1'))
      expect(screen.queryByText('Revision history')).not.toBeInTheDocument()
    })
  })

  describe.each([
    ['node', 'node-expansion:widget-a', 'pod//from-node'],
    ['namespace', 'namespace-expansion:widget-a', 'pod/widget-a/from-ns'],
    ['serviceaccount', 'sa-expansion:widget-a', 'pod/prod/from-sa'],
    ['consumers', 'consumers-expansion:widget-a', 'pod/prod/from-consumers'],
    ['workload-pods', 'workload-expansion:widget-a', 'pod/prod/from-workload'],
  ] as const)('customExpand=%s', (customExpand, expansionText, expectedTarget) => {
    it('renders the matching expansion and wires its onOpen through to the drawer', async () => {
      listMock.mockResolvedValue([{ name: 'widget-a', namespace: 'prod', status: 'Ready' }])
      const user = userEvent.setup()
      renderWithClient(<ResourceView def={makeDef({ customExpand })} ctx="c" ns="" />)

      await user.click(await screen.findByText('widget-a'))
      const expansion = await screen.findByText(expansionText)
      await user.click(expansion)
      expect(await screen.findByTestId('drawer-open')).toHaveTextContent(expectedTarget)
    })
  })
})
