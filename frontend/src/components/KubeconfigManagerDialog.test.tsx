import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { KubeconfigManagerDialog } from './KubeconfigManagerDialog'
import { setAppPrefs } from '@/lib/preferences'
import type { KubeconfigView } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string, fallback?: string) => fallback ?? key }))

const {
  kubeconfigViewMock,
  setCurrentContextMock,
  editKubeconfigContextMock,
  createKubeconfigContextMock,
  deleteKubeconfigContextMock,
  previewKubeconfigImportMock,
  commitKubeconfigImportMock,
  revealKubeconfigSecretMock,
  pingContextMock,
} = vi.hoisted(() => ({
  kubeconfigViewMock: vi.fn(),
  setCurrentContextMock: vi.fn(),
  editKubeconfigContextMock: vi.fn(),
  createKubeconfigContextMock: vi.fn(),
  deleteKubeconfigContextMock: vi.fn(),
  previewKubeconfigImportMock: vi.fn(),
  commitKubeconfigImportMock: vi.fn(),
  revealKubeconfigSecretMock: vi.fn(),
  pingContextMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  kubeconfigView: kubeconfigViewMock,
  setCurrentContext: setCurrentContextMock,
  editKubeconfigContext: editKubeconfigContextMock,
  createKubeconfigContext: createKubeconfigContextMock,
  deleteKubeconfigContext: deleteKubeconfigContextMock,
  previewKubeconfigImport: previewKubeconfigImportMock,
  commitKubeconfigImport: commitKubeconfigImportMock,
  revealKubeconfigSecret: revealKubeconfigSecretMock,
  pingContext: pingContextMock,
}))

const baseView: KubeconfigView = {
  currentContext: 'prod',
  configPaths: ['/home/user/.kube/config'],
  contexts: [
    { name: 'prod', cluster: 'prod-cluster', user: 'prod-user', namespace: 'default', locationOfOrigin: '/home/user/.kube/config', current: true },
    { name: 'staging', cluster: 'staging-cluster', user: 'staging-user', namespace: 'default', locationOfOrigin: '/home/user/.kube/config', current: false },
  ],
  clusters: [
    {
      name: 'prod-cluster',
      server: 'https://prod',
      locationOfOrigin: '/home/user/.kube/config',
      insecureSkipTLSVerify: false,
      hasCertificateAuthorityData: true,
    },
    {
      name: 'staging-cluster',
      server: 'https://staging',
      locationOfOrigin: '/home/user/.kube/config',
      insecureSkipTLSVerify: false,
      hasCertificateAuthorityData: true,
    },
  ],
  users: [
    {
      name: 'prod-user',
      locationOfOrigin: '/home/user/.kube/config',
      hasPassword: false,
      hasToken: true,
      hasClientCertificateData: false,
      hasClientKeyData: false,
    },
    {
      name: 'staging-user',
      locationOfOrigin: '/home/user/.kube/config',
      hasPassword: false,
      hasToken: false,
      hasClientCertificateData: false,
      hasClientKeyData: false,
    },
  ],
}

function renderDialog(overrides: Partial<Parameters<typeof KubeconfigManagerDialog>[0]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const onSelectContext = vi.fn()
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={qc}>
      <KubeconfigManagerDialog open={true} onClose={onClose} activeCtx="prod" onSelectContext={onSelectContext} {...overrides} />
    </QueryClientProvider>,
  )
  return { onSelectContext, onClose }
}

function prefs(): Record<string, unknown> {
  return JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}')
}

beforeEach(() => {
  localStorage.clear()
  setAppPrefs({
    mcp: { enabled: true, allowWrite: true, readOnlyContexts: [], readDisabledContexts: [] },
    contexts: { favorites: [] },
  })
  kubeconfigViewMock.mockReset().mockResolvedValue(structuredClone(baseView))
  setCurrentContextMock.mockReset().mockResolvedValue(undefined)
  editKubeconfigContextMock.mockReset().mockResolvedValue(undefined)
  createKubeconfigContextMock.mockReset().mockResolvedValue(undefined)
  deleteKubeconfigContextMock.mockReset().mockResolvedValue({})
  previewKubeconfigImportMock.mockReset()
  commitKubeconfigImportMock.mockReset().mockResolvedValue(undefined)
  revealKubeconfigSecretMock.mockReset()
  pingContextMock.mockReset()
})

function findRow(name: string) {
  return screen.getByText(name).closest('tr')!
}

describe('KubeconfigManagerDialog', () => {
  it('renders nothing when closed', () => {
    renderDialog({ open: false })
    expect(screen.queryByText('Contexts')).not.toBeInTheDocument()
  })

  it('lists contexts from the kubeconfig view, with the current one badged', async () => {
    renderDialog()
    await screen.findByText('Contexts')
    expect(within(findRow('prod')).getByText('prod-cluster')).toBeInTheDocument()
    expect(within(findRow('staging')).getByText('staging-cluster')).toBeInTheDocument()
    expect(within(findRow('prod')).getByText('current')).toBeInTheDocument()
    expect(within(findRow('staging')).queryByText('current')).not.toBeInTheDocument()
  })

  it('sets a context as current via its row action', async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    await user.click(within(findRow('staging')).getByTitle('Set as current context'))
    await waitFor(() => expect(setCurrentContextMock).toHaveBeenCalledWith('staging'))
  })

  it('favoriting a context updates app preferences and does not call the backend', async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    await user.click(within(findRow('staging')).getByLabelText('Favorite'))
    expect((prefs().contexts as { favorites: string[] }).favorites).toEqual(['staging'])
    expect(kubeconfigViewMock).toHaveBeenCalledTimes(1) // no extra refetch from a pure-local preference change
  })

  it('toggling the MCP read switch updates mcp.readDisabledContexts (inverted)', async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    const readSwitch = within(findRow('staging')).getByRole('switch', { name: 'MCP read — staging' })
    expect(readSwitch).toHaveAttribute('aria-checked', 'true') // nothing disabled yet
    await user.click(readSwitch)
    expect((prefs().mcp as { readDisabledContexts: string[] }).readDisabledContexts).toEqual(['staging'])
  })

  it('toggling the MCP write switch updates mcp.readOnlyContexts (inverted)', async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    const writeSwitch = within(findRow('staging')).getByRole('switch', { name: 'MCP write — staging' })
    await user.click(writeSwitch)
    expect((prefs().mcp as { readOnlyContexts: string[] }).readOnlyContexts).toEqual(['staging'])
  })

  it('MCP switches are disabled when the corresponding global gate is off', async () => {
    setAppPrefs({ mcp: { enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] } })
    renderDialog()
    await screen.findByText('Contexts')

    expect(within(findRow('staging')).getByRole('switch', { name: 'MCP read — staging' })).toBeDisabled()
    expect(within(findRow('staging')).getByRole('switch', { name: 'MCP write — staging' })).toBeDisabled()
  })

  it('renames a context and, when it is the active one, notifies the parent to follow the rename', async () => {
    const user = userEvent.setup()
    const { onSelectContext } = renderDialog({ activeCtx: 'prod' })
    await screen.findByText('Contexts')

    // Capture the row once, before "prod" stops being plain text (it
    // becomes an <input> value once editing starts) — findRow relies on a
    // text-content lookup that would no longer match afterward.
    const row = findRow('prod')
    await user.click(within(row).getByTitle('Edit'))
    const nameInput = within(row).getByDisplayValue('prod')
    await user.clear(nameInput)
    await user.type(nameInput, 'prod-renamed')
    await user.click(within(row).getByLabelText('Save'))

    await waitFor(() => expect(editKubeconfigContextMock).toHaveBeenCalledWith('prod', { newName: 'prod-renamed', namespace: 'default' }))
    expect(onSelectContext).toHaveBeenCalledWith('prod-renamed')
  })

  it('deletes a context after confirmation and surfaces an orphan notice', async () => {
    deleteKubeconfigContextMock.mockResolvedValue({ orphanedCluster: 'staging-cluster', orphanedUser: 'staging-user' })
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    await user.click(within(findRow('staging')).getByTitle('Delete'))
    await user.click(within(findRow('staging')).getByText('Confirm'))

    await waitFor(() => expect(deleteKubeconfigContextMock).toHaveBeenCalledWith('staging'))
    expect(await screen.findByText('cluster "staging-cluster" and user "staging-user" are no longer used by any context')).toBeInTheDocument()
  })

  it('creates a new context from an existing cluster and user', async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    await user.type(screen.getByLabelText('Name'), 'new-ctx')
    await user.selectOptions(screen.getByLabelText('Cluster'), 'staging-cluster')
    await user.selectOptions(screen.getByLabelText('User'), 'staging-user')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() =>
      expect(createKubeconfigContextMock).toHaveBeenCalledWith({ name: 'new-ctx', cluster: 'staging-cluster', user: 'staging-user', namespace: undefined }),
    )
  })

  it('reveals a secret on demand via an audited round-trip, not a locally-cached value', async () => {
    revealKubeconfigSecretMock.mockResolvedValue('the-real-token')
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    expect(screen.queryByText('the-real-token')).not.toBeInTheDocument()
    await user.click(screen.getByText('Token'))
    await waitFor(() => expect(revealKubeconfigSecretMock).toHaveBeenCalledWith('prod-user', 'token'))
    expect(await screen.findByText('the-real-token')).toBeInTheDocument()
  })

  it('previews then commits a kubeconfig import', async () => {
    previewKubeconfigImportMock.mockResolvedValue({
      addedContexts: ['new'],
      addedClusters: [],
      addedUsers: [],
      conflictingContexts: [],
      conflictingClusters: [],
      conflictingUsers: [],
    })
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText('Contexts')

    await user.click(screen.getByText('Import another kubeconfig'))
    await user.type(screen.getByPlaceholderText('Paste a kubeconfig YAML here'), 'apiVersion: v1')
    await user.click(screen.getByText('Preview'))

    await waitFor(() => expect(previewKubeconfigImportMock).toHaveBeenCalledWith('apiVersion: v1'))
    expect(await screen.findByText(/Will add/)).toBeInTheDocument()

    await user.click(screen.getByText('Commit merge'))
    await waitFor(() => expect(commitKubeconfigImportMock).toHaveBeenCalledWith('apiVersion: v1', []))
  })
})
