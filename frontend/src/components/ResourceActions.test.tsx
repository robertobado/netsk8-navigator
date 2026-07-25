import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ResourceActions } from './ResourceActions'

// Identity translator — decouples these tests from i18n dictionary content.
vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { deleteResourceMock, scaleResourceMock, restartRolloutMock, getDetailMock, rolloutHistoryMock, rolloutUndoMock } = vi.hoisted(() => ({
  deleteResourceMock: vi.fn(),
  scaleResourceMock: vi.fn(),
  restartRolloutMock: vi.fn(),
  getDetailMock: vi.fn(),
  rolloutHistoryMock: vi.fn(),
  rolloutUndoMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  deleteResource: deleteResourceMock,
  scaleResource: scaleResourceMock,
  restartRollout: restartRolloutMock,
  getDetail: getDetailMock,
  rolloutHistory: rolloutHistoryMock,
  rolloutUndo: rolloutUndoMock,
  SCALABLE_KINDS: new Set(['deployment', 'statefulset', 'replicaset']),
  RESTARTABLE_KINDS: new Set(['deployment', 'statefulset', 'daemonset']),
  HISTORY_KINDS: new Set(['deployment']),
}))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  deleteResourceMock.mockReset().mockResolvedValue(undefined)
  scaleResourceMock.mockReset().mockResolvedValue(undefined)
  restartRolloutMock.mockReset().mockResolvedValue(undefined)
  getDetailMock.mockReset().mockResolvedValue({ replicas: 2 })
  rolloutHistoryMock.mockReset().mockResolvedValue([])
  rolloutUndoMock.mockReset().mockResolvedValue(undefined)
})

describe('ResourceActions', () => {
  it('renders nothing when the drawer is read-only', () => {
    const { container } = renderWithClient(<ResourceActions ctx="c" kind="deployment" namespace="ns" name="web" editable={false} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('only shows Delete for a kind that cannot be scaled or restarted', () => {
    renderWithClient(<ResourceActions ctx="c" kind="service" namespace="ns" name="web" editable={true} />)
    expect(screen.getByText('Delete')).toBeInTheDocument()
    expect(screen.queryByText('Scale')).not.toBeInTheDocument()
    expect(screen.queryByText('Restart rollout')).not.toBeInTheDocument()
  })

  it('shows Scale and Restart rollout for a deployment', async () => {
    renderWithClient(<ResourceActions ctx="c" kind="deployment" namespace="ns" name="web" editable={true} />)
    expect(await screen.findByText('Restart rollout')).toBeInTheDocument()
    expect(screen.getByLabelText('Replicas')).toBeInTheDocument()
  })

  it('requires typing the exact name before delete can be confirmed', async () => {
    const user = userEvent.setup()
    const onDeleted = vi.fn()
    renderWithClient(<ResourceActions ctx="c" kind="service" namespace="ns" name="web" editable={true} onDeleted={onDeleted} />)

    await user.click(screen.getByText('Delete'))
    const confirmButton = screen.getByText('Confirm')
    expect(confirmButton).toBeDisabled()

    const input = screen.getByPlaceholderText('web')
    await user.type(input, 'wrong-name')
    expect(confirmButton).toBeDisabled()

    await user.clear(input)
    await user.type(input, 'web')
    expect(confirmButton).not.toBeDisabled()

    await user.click(confirmButton)
    expect(deleteResourceMock).toHaveBeenCalledWith('c', 'service', 'ns', 'web')
    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
  })

  it('cancelling the delete confirmation clears the typed name', async () => {
    const user = userEvent.setup()
    renderWithClient(<ResourceActions ctx="c" kind="service" namespace="ns" name="web" editable={true} />)

    await user.click(screen.getByText('Delete'))
    await user.type(screen.getByPlaceholderText('web'), 'web')
    await user.click(screen.getByText('Cancel'))

    // Back to the idle state — the confirm panel (and its input) is gone.
    expect(screen.queryByPlaceholderText('web')).not.toBeInTheDocument()
    expect(deleteResourceMock).not.toHaveBeenCalled()
  })

  it('scales after confirming, seeding the input from the current replica count', async () => {
    const user = userEvent.setup()
    renderWithClient(<ResourceActions ctx="c" kind="deployment" namespace="ns" name="web" editable={true} />)

    const input = await screen.findByLabelText('Replicas')
    await waitFor(() => expect(input).toHaveValue(2))

    await user.clear(input)
    await user.type(input, '5')
    await user.click(screen.getByText('Scale'))
    await user.click(screen.getByText('Confirm'))

    await waitFor(() => expect(scaleResourceMock).toHaveBeenCalledWith('c', 'deployment', 'ns', 'web', 5))
  })

  it('restarts the rollout after confirming', async () => {
    const user = userEvent.setup()
    renderWithClient(<ResourceActions ctx="c" kind="deployment" namespace="ns" name="web" editable={true} />)

    await user.click(await screen.findByText('Restart rollout'))
    await user.click(screen.getByText('Confirm'))

    await waitFor(() => expect(restartRolloutMock).toHaveBeenCalledWith('c', 'deployment', 'ns', 'web'))
  })
})
