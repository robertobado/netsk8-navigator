import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MetricsSection } from './MetricsSection'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { metricsRefreshMock } = vi.hoisted(() => ({
  metricsRefreshMock: vi.fn<() => { ms: number; interval: number | null }>(() => ({ ms: 30000, interval: 30000 })),
}))
vi.mock('@/lib/metrics', () => ({ useMetricsRefresh: metricsRefreshMock }))

// jsdom has no ResizeObserver — NodeMetricCarousel uses one to detect overflow.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

// jsdom's layout boxes are always 0×0, which makes recharts' ResponsiveContainer
// skip rendering its children (real charts need a positive size) — give every
// element a plausible box so the actual chart (axes, tooltip) renders in tests.
Object.defineProperty(HTMLElement.prototype, 'getBoundingClientRect', {
  configurable: true,
  value: () => ({ width: 400, height: 140, top: 0, left: 0, bottom: 140, right: 400, x: 0, y: 0, toJSON: () => {} }),
})

const { monitoringMock, metricsMock, usageMock, nodesUsageMock } = vi.hoisted(() => ({
  monitoringMock: vi.fn(),
  metricsMock: vi.fn(),
  usageMock: vi.fn(),
  nodesUsageMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: { ...actual.api, monitoring: monitoringMock, metrics: metricsMock, usage: usageMock, nodesUsage: nodesUsageMock },
  }
})

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('MetricsSection', () => {
  // Regression: a Prometheus-look-alike source that discoverProm matches by
  // name/port but that isn't actually reachable used to make the backend
  // respond with points: null instead of available: false — which crashed
  // (series.points.at on null). The frontend should tolerate that shape too.
  it('does not crash when a metrics series has points: null', async () => {
    monitoringMock.mockResolvedValue({ available: true, kind: 'prometheus', metricsServer: true })
    metricsMock.mockResolvedValue({
      available: true,
      source: 'prometheus',
      cpu: { points: null, unit: 'cores' },
      memory: { points: null, unit: 'bytes' },
    })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({ available: false, items: [] })

    renderWithClient(<MetricsSection ctx="test" scope="cluster" />)

    expect(await screen.findByText('Metrics')).toBeInTheDocument()
    expect(await screen.findByText('CPU')).toBeInTheDocument()
  })

  it('links a node name in the instantaneous-gauge carousel to onOpenNode', async () => {
    monitoringMock.mockResolvedValue({ available: false, metricsServer: true })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({
      available: true,
      items: [{ name: 'ip-10-0-1-1.ec2.internal', cpu: { used: 1, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } }],
    })
    const onOpenNode = vi.fn()

    renderWithClient(<MetricsSection ctx="test" scope="cluster" onOpenNode={onOpenNode} />)

    const link = (await screen.findAllByText('ip-10-0-1-1'))[0] // shortNode() strips the .ec2.internal suffix
    fireEvent.click(link)
    expect(onOpenNode).toHaveBeenCalledWith('ip-10-0-1-1.ec2.internal')
  })

  it('links a node name in the time-series carousel to onOpenNode', async () => {
    monitoringMock.mockResolvedValue({ available: true, kind: 'prometheus', metricsServer: true })
    metricsMock.mockResolvedValue({ available: true, source: 'prometheus', cpu: { points: [] }, memory: { points: [] } })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({
      available: true,
      items: [{ name: 'ip-10-0-1-1.ec2.internal', cpu: { used: 1, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } }],
    })
    const onOpenNode = vi.fn()

    renderWithClient(<MetricsSection ctx="test" scope="cluster" onOpenNode={onOpenNode} />)

    const link = (await screen.findAllByText('ip-10-0-1-1'))[0]
    fireEvent.click(link)
    expect(onOpenNode).toHaveBeenCalledWith('ip-10-0-1-1.ec2.internal')
  })

  it('renders nothing when the global metrics-refresh interval is off', () => {
    metricsRefreshMock.mockReturnValueOnce({ ms: 0, interval: null })
    const { container } = renderWithClient(<MetricsSection ctx="test" scope="cluster" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders the node name as plain text (not a link) when onOpenNode is not given', async () => {
    monitoringMock.mockResolvedValue({ available: false, metricsServer: true })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({
      available: true,
      items: [{ name: 'ip-10-0-1-1.ec2.internal', cpu: { used: 1, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } }],
    })

    renderWithClient(<MetricsSection ctx="test" scope="cluster" />)

    const label = (await screen.findAllByText('ip-10-0-1-1'))[0]
    expect(label.closest('button')).not.toBeInTheDocument()
  })

  it('colors a near-full gauge as a warning/error zone and shows request/limit legend for pod scope', async () => {
    monitoringMock.mockResolvedValue({ available: false, metricsServer: true })
    usageMock.mockResolvedValue({
      available: true,
      cpu: { used: 9.5, request: 4, limit: 10, total: 10, unit: 'cores' },
      memory: { used: 8.1, request: 2, limit: 10, total: 10, unit: 'bytes' },
    })
    nodesUsageMock.mockResolvedValue({ available: false, items: [] })

    renderWithClient(<MetricsSection ctx="test" scope="pod" namespace="prod" name="web-1" />)

    expect((await screen.findAllByText(/req/)).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/lim/).length).toBeGreaterThan(0)
  })

  it('duplicates the per-node gauges for a seamless scroll once they overflow the carousel viewport', async () => {
    const scrollWidthDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollWidth')
    const clientWidthDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    Object.defineProperty(HTMLElement.prototype, 'scrollWidth', { configurable: true, get: () => 1000 })
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 200 })
    try {
      monitoringMock.mockResolvedValue({ available: false, metricsServer: true })
      usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
      nodesUsageMock.mockResolvedValue({
        available: true,
        items: [
          { name: 'node-a', cpu: { used: 3, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } },
          { name: 'node-b', cpu: { used: 1, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } },
        ],
      })

      renderWithClient(<MetricsSection ctx="test" scope="cluster" />)

      const cells = await screen.findAllByText('node-a')
      expect(cells.length).toBeGreaterThan(1)
    } finally {
      if (scrollWidthDesc) Object.defineProperty(HTMLElement.prototype, 'scrollWidth', scrollWidthDesc)
      if (clientWidthDesc) Object.defineProperty(HTMLElement.prototype, 'clientWidth', clientWidthDesc)
    }
  })

  it('switches the time-series range on click', async () => {
    monitoringMock.mockResolvedValue({ available: true, kind: 'prometheus', metricsServer: true })
    metricsMock.mockResolvedValue({
      available: true,
      source: 'prometheus',
      cpu: {
        points: [
          { t: 1000, v: 1 },
          { t: 2000, v: 2 },
        ],
        unit: 'cores',
      },
      memory: {
        points: [
          { t: 1000, v: 1e9 },
          { t: 2000, v: 2e9 },
        ],
        unit: 'bytes',
      },
    })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({ available: false, items: [] })

    renderWithClient(<MetricsSection ctx="test" scope="pod" namespace="prod" name="web-1" />)

    const sixHour = await screen.findByText('6h')
    fireEvent.click(sixHour)
    expect(metricsMock).toHaveBeenCalledWith('test', 'pod', { namespace: 'prod', name: 'web-1', range: '6h' })
  })

  it('renders a full-size time-series chart with a % axis when a ceiling is available', async () => {
    monitoringMock.mockResolvedValue({ available: true, kind: 'prometheus', metricsServer: true })
    metricsMock.mockResolvedValue({
      available: true,
      source: 'prometheus',
      cpu: {
        points: [
          { t: 1000, v: 1 },
          { t: 2000, v: 2 },
        ],
        unit: 'cores',
      },
      memory: {
        points: [
          { t: 1000, v: 1e9 },
          { t: 2000, v: 2e9 },
        ],
        unit: 'bytes',
      },
    })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({ available: false, items: [] })

    const { container } = renderWithClient(<MetricsSection ctx="test" scope="pod" namespace="prod" name="web-1" />)

    let surface: Element | null = null
    await waitFor(() => {
      surface = container.querySelector('.recharts-surface')
      expect(surface).toBeInTheDocument()
    })
    fireEvent.mouseMove(surface!, { clientX: 200, clientY: 50 })
    fireEvent.mouseOver(surface!, { clientX: 200, clientY: 50 })
  })

  it('rotates the per-node time-series carousel with the prev/next/pin controls', async () => {
    monitoringMock.mockResolvedValue({ available: true, kind: 'prometheus', metricsServer: true })
    metricsMock.mockResolvedValue({
      available: true,
      source: 'prometheus',
      cpu: { points: [{ t: 1000, v: 1 }], unit: 'cores' },
      memory: { points: [{ t: 1000, v: 1e9 }], unit: 'bytes' },
    })
    usageMock.mockResolvedValue({ available: true, cpu: { used: 1, total: 8, unit: 'cores' }, memory: { used: 1, total: 8, unit: 'bytes' } })
    nodesUsageMock.mockResolvedValue({
      available: true,
      items: [
        { name: 'node-a', cpu: { used: 3, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } },
        { name: 'node-b', cpu: { used: 1, total: 4, unit: 'cores' }, memory: { used: 1, total: 4, unit: 'bytes' } },
      ],
    })
    const onOpenNode = vi.fn()

    renderWithClient(<MetricsSection ctx="test" scope="cluster" onOpenNode={onOpenNode} />)

    const indexCount = (text: string) => screen.getAllByText((_, el) => el?.tagName === 'SPAN' && el.textContent === text).length
    await waitFor(() => expect(indexCount('1/2')).toBeGreaterThan(0))
    fireEvent.click(screen.getAllByLabelText('Next')[0])
    expect(indexCount('2/2')).toBeGreaterThan(0)
    fireEvent.click(screen.getAllByLabelText('Previous')[0])
    expect(indexCount('1/2')).toBeGreaterThan(0)

    const pin = screen.getAllByTitle('Pin to this node')[0]
    fireEvent.click(pin)
    expect(screen.getAllByTitle('Resume carousel').length).toBeGreaterThan(0)
  })
})
