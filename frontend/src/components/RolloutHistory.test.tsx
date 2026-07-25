import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RolloutHistory } from './RolloutHistory'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { rolloutHistoryMock, rolloutUndoMock } = vi.hoisted(() => ({
  rolloutHistoryMock: vi.fn(),
  rolloutUndoMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  rolloutHistory: rolloutHistoryMock,
  rolloutUndo: rolloutUndoMock,
}))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  rolloutHistoryMock.mockReset().mockResolvedValue([
    { revision: 2, images: ['app:v2'], createdAt: '2026-01-02T00:00:00Z', current: true },
    { revision: 1, images: ['app:v1'], createdAt: '2026-01-01T00:00:00Z', current: false },
  ])
  rolloutUndoMock.mockReset().mockResolvedValue(undefined)
})

describe('RolloutHistory', () => {
  it('renders nothing when closed', () => {
    const { container } = renderWithClient(<RolloutHistory ctx="c" namespace="ns" name="web" open={false} onClose={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('lists revisions with a Current badge on the active one', async () => {
    renderWithClient(<RolloutHistory ctx="c" namespace="ns" name="web" open={true} onClose={vi.fn()} />)
    expect(await screen.findByText('app:v2')).toBeInTheDocument()
    expect(screen.getByText('app:v1')).toBeInTheDocument()
    expect(screen.getByText('Current')).toBeInTheDocument()
    // Only the older, non-current revision gets an Undo button.
    expect(screen.getAllByText('Undo')).toHaveLength(1)
  })

  it('undoes to the chosen revision after confirming', async () => {
    const user = userEvent.setup()
    renderWithClient(<RolloutHistory ctx="c" namespace="ns" name="web" open={true} onClose={vi.fn()} />)

    await user.click(await screen.findByText('Undo'))
    await user.click(screen.getByText('Confirm'))

    await waitFor(() => expect(rolloutUndoMock).toHaveBeenCalledWith('c', 'deployment', 'ns', 'web', 1))
  })
})
