import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventsView } from './EventsView'
import type { EventView } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key, tAgo: (_t: unknown, s: string) => s }))

const { allEventsMock } = vi.hoisted(() => ({ allEventsMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, allEvents: allEventsMock } }
})

function ev(overrides: Partial<EventView> = {}): EventView {
  return {
    type: 'Normal',
    reason: 'Scheduled',
    message: 'Successfully assigned default/web-1 to node-1',
    count: 1,
    first: '2026-01-01T00:00:00Z',
    last: '2026-01-01T00:00:00Z',
    source: 'default-scheduler',
    objectKind: 'Pod',
    objectNamespace: 'default',
    objectName: 'web-1',
    ...overrides,
  }
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('EventsView', () => {
  it('shows an error state with a retry button when the fetch fails', async () => {
    allEventsMock.mockRejectedValue(new Error('exec: no such credential helper'))
    renderWithClient(<EventsView ctx="test" ns="" onOpen={vi.fn()} />)
    expect(await screen.findByText('Could not load events for this cluster.')).toBeInTheDocument()
    expect(screen.getByText(/expired credential or no permission/)).toBeInTheDocument()
  })

  it('shows an empty state when there are no events', async () => {
    allEventsMock.mockResolvedValue([])
    renderWithClient(<EventsView ctx="test" ns="" onOpen={vi.fn()} />)
    expect(await screen.findByText('No events found.')).toBeInTheDocument()
  })

  it('lists events, counts warnings, and opens the involved object on click', async () => {
    const onOpen = vi.fn()
    allEventsMock.mockResolvedValue([
      ev({ reason: 'Scheduled' }),
      ev({ type: 'Warning', reason: 'BackOff', message: 'Back-off restarting failed container', count: 3, objectName: 'web-2' }),
    ])
    renderWithClient(<EventsView ctx="test" ns="" onOpen={onOpen} />)

    expect(await screen.findByText('Scheduled')).toBeInTheDocument()
    expect(screen.getByText('BackOff')).toBeInTheDocument()
    expect(screen.getByText('×3')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /default\/web-1/ }))
    expect(onOpen).toHaveBeenCalledWith({ kind: 'pod', namespace: 'default', name: 'web-1' })
  })

  it('filters to warnings only and supports free-text search', async () => {
    allEventsMock.mockResolvedValue([
      ev({ reason: 'Scheduled' }),
      ev({ type: 'Warning', reason: 'FailedMount', message: 'Unable to attach or mount volumes', objectName: 'web-2' }),
    ])
    renderWithClient(<EventsView ctx="test" ns="" onOpen={vi.fn()} />)
    expect(await screen.findByText('Scheduled')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Warnings'))
    expect(screen.queryByText('Scheduled')).not.toBeInTheDocument()
    expect(screen.getByText('FailedMount')).toBeInTheDocument()

    fireEvent.click(screen.getByText('All', { exact: false }))
    fireEvent.change(screen.getByPlaceholderText('Filter by reason, object, or message...'), { target: { value: 'mount' } })
    expect(screen.queryByText('Scheduled')).not.toBeInTheDocument()
    expect(screen.getByText('FailedMount')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Filter by reason, object, or message...'), { target: { value: 'nothing matches this' } })
    expect(screen.getByText('No events found.')).toBeInTheDocument()
  })

  it('shows a non-clickable object label when the kind has no known drawer slug', async () => {
    allEventsMock.mockResolvedValue([ev({ objectKind: 'SomeCRD', objectName: 'thing-1', objectNamespace: 'default' })])
    renderWithClient(<EventsView ctx="test" ns="" onOpen={vi.fn()} />)
    expect(await screen.findByText('Scheduled')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /default\/thing-1/ })).not.toBeInTheDocument()
    expect(screen.getByText('default/thing-1', { exact: false })).toBeInTheDocument()
  })

  it('caps the visible list and shows a "showing N of M" notice past the cap', async () => {
    const many = Array.from({ length: 401 }, (_, i) => ev({ reason: `Reason${i}`, objectName: `pod-${i}` }))
    allEventsMock.mockResolvedValue(many)
    renderWithClient(<EventsView ctx="test" ns="" onOpen={vi.fn()} />)
    expect(await screen.findByText('Reason0')).toBeInTheDocument()
    expect(screen.getByText(/most recent events out of/)).toBeInTheDocument()
    expect(screen.queryByText('Reason400')).not.toBeInTheDocument()
  })
})
