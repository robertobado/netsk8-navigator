import type { ReactElement } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MultiPodLogsPanel } from './MultiPodLogsPanel'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { workloadPodsMock } = vi.hoisted(() => ({ workloadPodsMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, workloadPods: workloadPodsMock } }
})

// jsdom has no EventSource — stand in with a controllable fake so tests can
// drive onmessage directly instead of needing a real backend stream.
class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  onmessage: ((e: MessageEvent) => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  close() {}
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  workloadPodsMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('MultiPodLogsPanel', () => {
  it('shows an empty state when the workload has no pods', async () => {
    workloadPodsMock.mockResolvedValue([])
    renderWithClient(<MultiPodLogsPanel ctx="c" kind="deployment" namespace="prod" name="web" />)
    expect(await screen.findByText('No pods for this workload.')).toBeInTheDocument()
  })

  it('opens one aggregated stream for the workload and renders lines tagged by pod', async () => {
    workloadPodsMock.mockResolvedValue([
      { name: 'web-1', namespace: 'prod', containers: ['app'] },
      { name: 'web-2', namespace: 'prod', containers: ['app'] },
    ])
    renderWithClient(<MultiPodLogsPanel ctx="c" kind="deployment" namespace="prod" name="web" />)

    await screen.findByText('web-1')
    await screen.findByText('web-2')
    expect(FakeEventSource.instances).toHaveLength(1)
    expect(FakeEventSource.instances[0].url).toBe('/api/contexts/c/pods-of/deployment/prod/web/logs?container=app')

    FakeEventSource.instances[0].emit({ pod: 'web-1', line: 'hello from web-1' })
    await waitFor(() => expect(screen.getByText('hello from web-1')).toBeInTheDocument())
  })

  it('filters out a pod when its chip is toggled off', async () => {
    workloadPodsMock.mockResolvedValue([{ name: 'web-1', namespace: 'prod', containers: ['app'] }])
    const { default: userEvent } = await import('@testing-library/user-event')
    const user = userEvent.setup()
    renderWithClient(<MultiPodLogsPanel ctx="c" kind="deployment" namespace="prod" name="web" />)

    const chip = await screen.findByRole('button', { name: 'web-1' })
    FakeEventSource.instances[0].emit({ pod: 'web-1', line: 'visible line' })
    await waitFor(() => expect(screen.getByText('visible line')).toBeInTheDocument())

    await user.click(chip)
    expect(screen.queryByText('visible line')).not.toBeInTheDocument()
  })
})
