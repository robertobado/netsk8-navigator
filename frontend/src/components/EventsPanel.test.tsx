import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventsPanel } from './EventsPanel'
import type { EventView } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key, tAgo: (_t: unknown, s: string) => s }))

const { eventsMock } = vi.hoisted(() => ({ eventsMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, events: eventsMock } }
})

function ev(overrides: Partial<EventView> = {}): EventView {
  return { type: 'Normal', reason: 'Scheduled', message: 'assigned to node-1', count: 1, first: '', last: '2026-01-01T00:00:00Z', source: 'scheduler', ...overrides }
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('EventsPanel', () => {
  it('shows an empty state when there are no events', async () => {
    eventsMock.mockResolvedValue([])
    renderWithClient(<EventsPanel ctx="test" namespace="default" name="web-1" kind="Pod" />)
    await waitFor(() => expect(screen.getByText('No recent events for this pod.')).toBeInTheDocument())
    expect(eventsMock).toHaveBeenCalledWith('test', 'default', 'web-1', 'Pod')
  })

  it('lists events, marking Warning ones and showing the repeat count and source', async () => {
    eventsMock.mockResolvedValue([ev({ reason: 'Scheduled' }), ev({ type: 'Warning', reason: 'BackOff', message: 'restarting', count: 4, source: 'kubelet' })])
    renderWithClient(<EventsPanel ctx="test" namespace="default" name="web-1" />)
    await waitFor(() => expect(screen.getByText('Scheduled')).toBeInTheDocument())
    expect(screen.getByText('BackOff')).toBeInTheDocument()
    expect(screen.getByText('×4')).toBeInTheDocument()
    expect(screen.getByText('kubelet')).toBeInTheDocument()
  })
})
