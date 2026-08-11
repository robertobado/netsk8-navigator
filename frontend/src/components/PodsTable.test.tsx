import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PodsTable, TerminatingStatus } from './PodsTable'
import type { EventView, Pod } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { eventsMock, podsUsageMock, podPendingMock } = vi.hoisted(() => ({
  eventsMock: vi.fn(),
  podsUsageMock: vi.fn(),
  podPendingMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, events: eventsMock, podsUsage: podsUsageMock, podPending: podPendingMock } }
})

function warningEvent(reason: string, last: string): EventView {
  return { type: 'Warning', reason, message: `${reason} happened`, count: 1, first: last, last, source: 'kubelet' }
}

function pod(overrides: Partial<Pod> = {}): Pod {
  return {
    name: 'web-1',
    namespace: 'default',
    status: 'Running',
    ready: 1,
    total: 1,
    restarts: 0,
    node: 'node-1',
    ip: '10.0.0.5',
    age: new Date(Date.now() - 3_600_000).toISOString(),
    containers: ['app'],
    ownerKind: 'Deployment',
    ownerName: 'web',
    reason: '',
    deletedAt: '',
    finalizers: [],
    ...overrides,
  }
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('TerminatingStatus', () => {
  // Regression: a Warning event only had to overlap the deletion window in
  // time to count as a "termination problem", regardless of its reason —
  // so an unrelated, recurring warning (e.g. a sidecar failing to pull its
  // image secret) got flagged as if the *termination itself* were stuck.
  it('does not flag an unrelated Warning event that merely overlaps the termination window', async () => {
    const deletedAt = new Date().toISOString()
    eventsMock.mockResolvedValue([warningEvent('FailedToRetrieveImagePullSecret', deletedAt)])
    renderWithClient(<TerminatingStatus ctx="test" namespace="prod" name="web-1" deletedAt={deletedAt} finalizers={[]} />)

    await waitFor(() => expect(eventsMock).toHaveBeenCalled())
    expect(screen.queryByText('Termination problems')).not.toBeInTheDocument()
  })

  it('flags a Warning event whose reason is actually termination-related', async () => {
    const deletedAt = new Date().toISOString()
    eventsMock.mockResolvedValue([warningEvent('FailedKillPod', deletedAt)])
    const { container } = renderWithClient(<TerminatingStatus ctx="test" namespace="prod" name="web-1" deletedAt={deletedAt} finalizers={[]} />)

    // The badge only gets wrapped in a hoverable HoverBubble once the events
    // query resolves and `warn` flips true (shown by the alert-triangle icon
    // replacing the idle bouncing dots) — hovering any earlier lands on the
    // plain, non-hoverable badge that's about to be swapped out from under it.
    await waitFor(() => expect(container.querySelector('.lucide-triangle-alert')).toBeInTheDocument())
    fireEvent.mouseOver(screen.getByText('Terminating'))

    await waitFor(() => expect(screen.getByText('Termination problems')).toBeInTheDocument())
  })
})

describe('PodsTable', () => {
  it('classifies status per pod: Running pulse, error reason pill, Completed as ok, and default Pending', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    renderWithClient(
      <PodsTable
        ctx="test"
        pods={[
          pod({ name: 'running-pod', status: 'Running' }),
          pod({ name: 'crash-pod', status: 'Running', reason: 'CrashLoopBackOff' }),
          pod({ name: 'done-pod', status: 'Succeeded', reason: 'Completed' }),
          pod({ name: 'pending-pod', status: 'Pending' }),
        ]}
        connState="live"
        onOpenResource={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText('running-pod')).toBeInTheDocument())
    expect(screen.getByTitle('Active')).toBeInTheDocument() // Running's pulse dot
    expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    // Pending renders its badge status text inline (via PendingStatus wrapping the pill).
    expect(screen.getAllByText('Pending').length).toBeGreaterThan(0)
  })

  it('colors Ready/Restarts and treats a finished 0/1 job pod as healthy', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    renderWithClient(
      <PodsTable
        ctx="test"
        pods={[
          pod({ name: 'healthy', ready: 1, total: 1, restarts: 0 }),
          pod({ name: 'unhealthy', ready: 0, total: 1, restarts: 3 }),
          pod({ name: 'finished-job', status: 'Succeeded', reason: 'Completed', ready: 0, total: 1 }),
        ]}
        connState="live"
        onOpenResource={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText('healthy')).toBeInTheDocument())
    const readyCells = screen.getAllByText('0/1')
    // finished-job's "0/1" is healthy (green), unhealthy's "0/1" is not.
    expect(readyCells.some((c) => c.className.includes('--ok'))).toBe(true)
    expect(readyCells.some((c) => c.className.includes('--warn'))).toBe(true)
    const restarts = screen.getAllByText('3').find((el) => el.tagName === 'SPAN' && el.className.includes('font-mono'))
    expect(restarts?.className).toContain('--warn')
  })

  it('the ControlledBy link opens the owner resource without triggering the row click', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    const onSelect = vi.fn()
    const onOpenResource = vi.fn()
    renderWithClient(
      <PodsTable ctx="test" pods={[pod({ ownerKind: 'Deployment', ownerName: 'web' })]} connState="live" onSelect={onSelect} onOpenResource={onOpenResource} />,
    )
    await waitFor(() => expect(screen.getByText('web')).toBeInTheDocument())
    fireEvent.click(screen.getByText('web'))
    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'deployment', namespace: 'default', name: 'web', editable: false })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('the Node link opens the node resource; a pod with no node shows a dash', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    const onOpenResource = vi.fn()
    renderWithClient(
      <PodsTable
        ctx="test"
        pods={[pod({ name: 'scheduled', node: 'node-1' }), pod({ name: 'unscheduled', node: '' })]}
        connState="live"
        onOpenResource={onOpenResource}
      />,
    )
    await waitFor(() => expect(screen.getByText('node-1')).toBeInTheDocument())
    fireEvent.click(screen.getByText('node-1'))
    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'node', namespace: '', name: 'node-1', editable: false })
  })

  it('clicking a row (not a link) calls onSelect with that pod', async () => {
    const onSelect = vi.fn()
    podsUsageMock.mockResolvedValue({ available: false })
    const target = pod({ name: 'clickable' })
    renderWithClient(<PodsTable ctx="test" pods={[target]} connState="live" onSelect={onSelect} onOpenResource={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('clickable')).toBeInTheDocument())
    fireEvent.click(screen.getByText('clickable'))
    expect(onSelect).toHaveBeenCalledWith(target)
  })

  it('shows CPU/Mem usage columns and gauges when podsUsage is available', async () => {
    podsUsageMock.mockResolvedValue({
      available: true,
      items: { 'default/web-1': { cpu: { used: 0.5, total: 1, unit: 'cores' }, memory: { used: 500_000_000, total: 1_000_000_000, unit: 'bytes' } } },
    })
    renderWithClient(<PodsTable ctx="test" pods={[pod()]} connState="live" onOpenResource={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('CPU')).toBeInTheDocument())
    expect(screen.getByText('Mem')).toBeInTheDocument()
  })

  it('hovering a Pending pod fetches and shows its pending detail', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    podPendingMock.mockResolvedValue({ since: new Date().toISOString(), reason: 'Unschedulable', message: 'insufficient cpu' })
    renderWithClient(<PodsTable ctx="test" pods={[pod({ status: 'Pending' })]} connState="live" onOpenResource={vi.fn()} />)
    await waitFor(() => expect(screen.getAllByText('Pending').length).toBeGreaterThan(0))

    fireEvent.mouseEnter(screen.getAllByText('Pending')[0])
    await waitFor(() => expect(podPendingMock).toHaveBeenCalledWith('test', 'default', 'web-1'))
    await waitFor(() => expect(screen.getByText('insufficient cpu')).toBeInTheDocument())
  })

  it('LiveIndicator reflects the connection state', async () => {
    podsUsageMock.mockResolvedValue({ available: false })
    const { rerender } = renderWithClient(<PodsTable ctx="test" pods={[]} connState="connecting" onOpenResource={vi.fn()} />)
    expect(screen.getByText('connecting')).toBeInTheDocument()

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    rerender(
      <QueryClientProvider client={qc}>
        <PodsTable ctx="test" pods={[]} connState="error" onOpenResource={vi.fn()} />
      </QueryClientProvider>,
    )
    expect(screen.getByText('reconnecting')).toBeInTheDocument()
  })
})
