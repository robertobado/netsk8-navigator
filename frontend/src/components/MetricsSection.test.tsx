import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MetricsSection } from './MetricsSection'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('@/lib/metrics', () => ({ useMetricsRefresh: () => ({ ms: 30000, interval: 30000 }) }))

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
})
