import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TerminatingStatus } from './PodsTable'
import type { EventView } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { eventsMock } = vi.hoisted(() => ({ eventsMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, events: eventsMock } }
})

function warningEvent(reason: string, last: string): EventView {
  return { type: 'Warning', reason, message: `${reason} happened`, count: 1, first: last, last, source: 'kubelet' }
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
