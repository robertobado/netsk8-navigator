import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HelmInstallDialog } from './HelmInstallDialog'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('@/lib/monaco', () => ({}))
vi.mock('@/lib/monacoTheme', () => ({ NETSK8_THEME: 'test-theme', ensureNetsk8Theme: vi.fn() }))
vi.mock('@monaco-editor/react', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea aria-label="values" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}))

const { helmSearchMock, helmChartDetailMock, installHelmReleaseMock, upgradeHelmReleaseMock } = vi.hoisted(() => ({
  helmSearchMock: vi.fn(),
  helmChartDetailMock: vi.fn(),
  installHelmReleaseMock: vi.fn(),
  upgradeHelmReleaseMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    helmSearch: helmSearchMock,
    helmChartDetail: helmChartDetailMock,
    installHelmRelease: installHelmReleaseMock,
    upgradeHelmRelease: upgradeHelmReleaseMock,
  }
})

function renderDialog(props: Partial<React.ComponentProps<typeof HelmInstallDialog>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const defaults: React.ComponentProps<typeof HelmInstallDialog> = {
    ctx: 'c',
    mode: 'install',
    open: true,
    onClose: vi.fn(),
    onDone: vi.fn(),
  }
  return render(
    <QueryClientProvider client={qc}>
      <HelmInstallDialog {...defaults} {...props} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  helmSearchMock.mockReset().mockResolvedValue([{ repo: 'bitnami', name: 'nginx', version: '1.2.3', appVersion: '1.25.0', description: 'A web server' }])
  helmChartDetailMock.mockReset().mockResolvedValue({ versions: ['1.2.3', '1.2.2'], defaultValues: 'replicaCount: 1\n', readme: '' })
  installHelmReleaseMock.mockReset()
  upgradeHelmReleaseMock.mockReset()
})

describe('HelmInstallDialog', () => {
  it('renders nothing when closed', () => {
    const { container } = renderDialog({ open: false })
    expect(container).toBeEmptyDOMElement()
  })

  it('searches, selects a chart, and installs it', async () => {
    const onDone = vi.fn()
    installHelmReleaseMock.mockResolvedValue({
      name: 'my-nginx',
      namespace: 'default',
      chart: 'nginx-1.2.3',
      appVersion: '1.25.0',
      revision: 1,
      status: 'deployed',
      updated: '2026-01-01T00:00:00Z',
    })
    const user = userEvent.setup()
    renderDialog({ onDone })

    await user.type(screen.getByPlaceholderText('Search charts...'), 'nginx')
    const result = await screen.findByText(/bitnami\/nginx/)
    await user.click(result)

    await screen.findByLabelText('values')
    await user.type(screen.getByPlaceholderText('Release name'), 'my-nginx')

    await user.click(screen.getByText('Install'))

    await waitFor(() =>
      expect(installHelmReleaseMock).toHaveBeenCalledWith('c', {
        repo: 'bitnami',
        chart: 'nginx',
        version: '1.2.3',
        releaseName: 'my-nginx',
        namespace: 'default',
        values: 'replicaCount: 1\n',
      }),
    )
    expect(onDone).toHaveBeenCalled()
  })

  it('disables Install until a release name is entered', async () => {
    const user = userEvent.setup()
    renderDialog()

    await user.type(screen.getByPlaceholderText('Search charts...'), 'nginx')
    await user.click(await screen.findByText(/bitnami\/nginx/))
    await screen.findByLabelText('values')

    expect(screen.getByText('Install')).toBeDisabled()
    await user.type(screen.getByPlaceholderText('Release name'), 'x')
    expect(screen.getByText('Install')).not.toBeDisabled()
  })

  it('upgrade mode calls upgradeHelmRelease with the existing release identity', async () => {
    upgradeHelmReleaseMock.mockResolvedValue({
      name: 'web',
      namespace: 'prod',
      chart: 'nginx-1.2.3',
      appVersion: '1.25.0',
      revision: 2,
      status: 'deployed',
      updated: '2026-01-01T00:00:00Z',
    })
    const user = userEvent.setup()
    renderDialog({ mode: 'upgrade', existingRelease: { namespace: 'prod', name: 'web' }, initialValues: 'replicaCount: 5\n' })

    await user.type(screen.getByPlaceholderText('Search charts...'), 'nginx')
    await user.click(await screen.findByText(/bitnami\/nginx/))
    // Upgrade mode has no release-name/namespace inputs — button is enabled as soon as a chart is picked.
    await waitFor(() => expect(screen.getByText('Upgrade')).not.toBeDisabled())

    await user.click(screen.getByText('Upgrade'))
    await waitFor(() =>
      expect(upgradeHelmReleaseMock).toHaveBeenCalledWith('c', 'prod', 'web', {
        repo: 'bitnami',
        chart: 'nginx',
        version: '1.2.3',
        releaseName: '',
        namespace: 'default',
        values: 'replicaCount: 5\n',
      }),
    )
  })

  it('shows the backend error message on failure', async () => {
    installHelmReleaseMock.mockRejectedValue(new Error('release already exists'))
    const user = userEvent.setup()
    renderDialog()

    await user.type(screen.getByPlaceholderText('Search charts...'), 'nginx')
    await user.click(await screen.findByText(/bitnami\/nginx/))
    await user.type(screen.getByPlaceholderText('Release name'), 'my-nginx')
    await user.click(screen.getByText('Install'))

    expect(await screen.findByText('release already exists')).toBeInTheDocument()
  })
})
