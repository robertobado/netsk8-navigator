import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HelmView } from './HelmView'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { helmReleasesMock } = vi.hoisted(() => ({ helmReleasesMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, helmReleases: helmReleasesMock }
})
vi.mock('./HelmReleaseDrawer', () => ({ HelmReleaseDrawer: () => null }))
vi.mock('./HelmReposPanel', () => ({ HelmReposPanel: () => <div>repos-panel</div> }))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  helmReleasesMock.mockReset().mockResolvedValue([])
})

describe('HelmView', () => {
  it('shows the releases tab by default', async () => {
    helmReleasesMock.mockResolvedValue([
      { name: 'web', namespace: 'prod', chart: 'nginx-1.2.3', appVersion: '1.25.0', revision: 1, status: 'deployed', updated: '2026-01-01T00:00:00Z' },
    ])
    renderWithClient(<HelmView ctx="c" ns="" />)
    expect(await screen.findByText('web')).toBeInTheDocument()
    expect(screen.getByText('nginx-1.2.3')).toBeInTheDocument()
  })

  it('switches to the repositories tab', async () => {
    const user = userEvent.setup()
    renderWithClient(<HelmView ctx="c" ns="" />)
    await waitFor(() => expect(helmReleasesMock).toHaveBeenCalled())

    await user.click(screen.getByText('Repositories'))
    expect(await screen.findByText('repos-panel')).toBeInTheDocument()
  })

  it('shows an error state with a retry button when the list fails', async () => {
    helmReleasesMock.mockRejectedValue(new Error('connection refused'))
    renderWithClient(<HelmView ctx="c" ns="" />)
    expect(await screen.findByText('Could not load Helm releases.')).toBeInTheDocument()
    expect(screen.getByText('connection refused')).toBeInTheDocument()
    expect(screen.getByText('Try again')).toBeInTheDocument()
  })
})
