import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HelmReposPanel } from './HelmReposPanel'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { helmReposMock, addHelmRepoMock, removeHelmRepoMock, refreshHelmRepoMock } = vi.hoisted(() => ({
  helmReposMock: vi.fn(),
  addHelmRepoMock: vi.fn(),
  removeHelmRepoMock: vi.fn(),
  refreshHelmRepoMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    helmRepos: helmReposMock,
    addHelmRepo: addHelmRepoMock,
    removeHelmRepo: removeHelmRepoMock,
    refreshHelmRepo: refreshHelmRepoMock,
  }
})
// The install dialog lazy-loads Monaco — irrelevant to this panel's own logic.
vi.mock('./HelmInstallDialogLazy', () => ({ HelmInstallDialogLazy: () => null }))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  helmReposMock.mockReset().mockResolvedValue([])
  addHelmRepoMock.mockReset()
  removeHelmRepoMock.mockReset()
  refreshHelmRepoMock.mockReset()
})

describe('HelmReposPanel', () => {
  it('shows an empty state with no repos added', async () => {
    renderWithClient(<HelmReposPanel ctx="c" />)
    expect(await screen.findByText('No repositories added yet.')).toBeInTheDocument()
  })

  it('lists added repos and disables Install chart until at least one exists', async () => {
    helmReposMock.mockResolvedValue([{ name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' }])
    renderWithClient(<HelmReposPanel ctx="c" />)

    expect(await screen.findByText('bitnami')).toBeInTheDocument()
    expect(screen.getByText('https://charts.bitnami.com/bitnami')).toBeInTheDocument()
    expect(screen.getByText('Install chart')).not.toBeDisabled()
  })

  it('adds a repo and refetches the list', async () => {
    addHelmRepoMock.mockResolvedValue({ name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' })
    const user = userEvent.setup()
    renderWithClient(<HelmReposPanel ctx="c" />)
    await screen.findByText('No repositories added yet.')

    await user.type(screen.getByPlaceholderText('Repo name'), 'bitnami')
    await user.type(screen.getByPlaceholderText('https://charts.example.com'), 'https://charts.bitnami.com/bitnami')
    await user.click(screen.getByText('Add'))

    await waitFor(() => expect(addHelmRepoMock).toHaveBeenCalledWith('bitnami', 'https://charts.bitnami.com/bitnami'))
    expect(helmReposMock).toHaveBeenCalledTimes(2) // initial load + post-add refetch
  })

  it('shows the backend error message when adding a repo fails', async () => {
    addHelmRepoMock.mockRejectedValue(new Error('could not reach repo'))
    const user = userEvent.setup()
    renderWithClient(<HelmReposPanel ctx="c" />)
    await screen.findByText('No repositories added yet.')

    await user.type(screen.getByPlaceholderText('Repo name'), 'bad')
    await user.type(screen.getByPlaceholderText('https://charts.example.com'), 'http://nope')
    await user.click(screen.getByText('Add'))

    expect(await screen.findByText('could not reach repo')).toBeInTheDocument()
  })

  it('removes a repo', async () => {
    helmReposMock.mockResolvedValue([{ name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' }])
    removeHelmRepoMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderWithClient(<HelmReposPanel ctx="c" />)

    await screen.findByText('bitnami')
    await user.click(screen.getByTitle('Remove'))

    await waitFor(() => expect(removeHelmRepoMock).toHaveBeenCalledWith('bitnami'))
  })

  it('refreshes a repo', async () => {
    helmReposMock.mockResolvedValue([{ name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' }])
    refreshHelmRepoMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderWithClient(<HelmReposPanel ctx="c" />)

    await screen.findByText('bitnami')
    await user.click(screen.getByTitle('Refresh'))

    await waitFor(() => expect(refreshHelmRepoMock).toHaveBeenCalledWith('bitnami'))
  })
})
