import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HelmReleaseDrawer } from './HelmReleaseDrawer'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { statusMock, manifestMock, historyMock, rollbackMock, uninstallMock } = vi.hoisted(() => ({
  statusMock: vi.fn(),
  manifestMock: vi.fn(),
  historyMock: vi.fn(),
  rollbackMock: vi.fn(),
  uninstallMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    helmReleaseStatus: statusMock,
    helmReleaseManifest: manifestMock,
    helmReleaseHistory: historyMock,
    helmReleaseRollback: rollbackMock,
    helmReleaseUninstall: uninstallMock,
  }
})
vi.mock('./HelmInstallDialogLazy', () => ({ HelmInstallDialogLazy: () => null }))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  statusMock.mockReset().mockResolvedValue({
    name: 'web',
    namespace: 'prod',
    chart: 'nginx-1.2.3',
    appVersion: '1.25.0',
    revision: 2,
    status: 'deployed',
    updated: '2026-01-01T00:00:00Z',
    notes: 'Thanks for installing nginx.',
    values: 'replicaCount: 2\n',
  })
  manifestMock.mockReset().mockResolvedValue('apiVersion: v1\nkind: ConfigMap\n')
  historyMock.mockReset().mockResolvedValue([
    { name: 'web', namespace: 'prod', chart: 'nginx-1.2.3', appVersion: '1.25.0', revision: 2, status: 'deployed', updated: '2026-01-01T00:00:00Z' },
    { name: 'web', namespace: 'prod', chart: 'nginx-1.2.2', appVersion: '1.24.0', revision: 1, status: 'superseded', updated: '2025-12-01T00:00:00Z' },
  ])
  rollbackMock.mockReset().mockResolvedValue(undefined)
  uninstallMock.mockReset().mockResolvedValue(undefined)
})

describe('HelmReleaseDrawer', () => {
  it('renders nothing when no target is selected', () => {
    const { container } = renderWithClient(<HelmReleaseDrawer ctx="c" target={null} onClose={vi.fn()} onChanged={vi.fn()} />)
    expect(container.querySelector('h2')).not.toBeInTheDocument()
  })

  it('shows the release values by default', async () => {
    renderWithClient(<HelmReleaseDrawer ctx="c" target={{ namespace: 'prod', name: 'web' }} onClose={vi.fn()} onChanged={vi.fn()} />)
    expect(await screen.findByText('replicaCount: 2')).toBeInTheDocument()
  })

  it('fetches the manifest lazily when the Manifest tab is opened', async () => {
    const user = userEvent.setup()
    renderWithClient(<HelmReleaseDrawer ctx="c" target={{ namespace: 'prod', name: 'web' }} onClose={vi.fn()} onChanged={vi.fn()} />)
    await screen.findByText('replicaCount: 2')
    expect(manifestMock).not.toHaveBeenCalled()

    await user.click(screen.getByText('Manifest'))
    await waitFor(() => expect(manifestMock).toHaveBeenCalledWith('c', 'prod', 'web'))
    expect(await screen.findByText(/kind: ConfigMap/)).toBeInTheDocument()
  })

  it('shows revision history and rolls back to an older revision', async () => {
    const onChanged = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<HelmReleaseDrawer ctx="c" target={{ namespace: 'prod', name: 'web' }} onClose={vi.fn()} onChanged={onChanged} />)
    await screen.findByText('replicaCount: 2')

    await user.click(screen.getByText('History'))
    expect(await screen.findByText('current')).toBeInTheDocument()
    await user.click(screen.getByText('Undo'))

    await waitFor(() => expect(rollbackMock).toHaveBeenCalledWith('c', 'prod', 'web', 1))
    expect(onChanged).toHaveBeenCalled()
  })

  it('uninstalls the release after confirming', async () => {
    const onClose = vi.fn()
    const onChanged = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<HelmReleaseDrawer ctx="c" target={{ namespace: 'prod', name: 'web' }} onClose={onClose} onChanged={onChanged} />)
    await screen.findByText('replicaCount: 2')

    await user.click(screen.getByText('Uninstall'))
    await user.click(screen.getByText('Confirm'))

    await waitFor(() => expect(uninstallMock).toHaveBeenCalledWith('c', 'prod', 'web'))
    expect(onChanged).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  it('cancelling the uninstall confirmation does not call the API', async () => {
    const user = userEvent.setup()
    renderWithClient(<HelmReleaseDrawer ctx="c" target={{ namespace: 'prod', name: 'web' }} onClose={vi.fn()} onChanged={vi.fn()} />)
    await screen.findByText('replicaCount: 2')

    await user.click(screen.getByText('Uninstall'))
    await user.click(screen.getByText('Cancel'))

    expect(uninstallMock).not.toHaveBeenCalled()
    expect(screen.getByText('Uninstall')).toBeInTheDocument()
  })
})
