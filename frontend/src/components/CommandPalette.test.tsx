import type { ReactElement } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CommandPalette } from './CommandPalette'

// jsdom has no ResizeObserver — cmdk's list uses one to keep the selected item
// in view, which this test doesn't care about.
class FakeResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// Identity translator — decouples these tests from i18n dictionary content.
vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { podsMock } = vi.hoisted(() => ({ podsMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, pods: podsMock } }
})

function renderWithClient(ui: ReactElement, qc: QueryClient) {
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  // jsdom doesn't implement this either — cmdk scrolls the active item into view.
  Element.prototype.scrollIntoView = vi.fn()
  podsMock.mockReset().mockResolvedValue([])
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('CommandPalette', () => {
  it('shows the navigate and switch-cluster groups by default', () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    renderWithClient(
      <CommandPalette
        open={true}
        onOpenChange={vi.fn()}
        contexts={[{ name: 'prod', cluster: 'prod', user: 'prod', namespace: '', server: '', current: true }]}
        selectedCtx="prod"
        onNavigate={vi.fn()}
        onSelectContext={vi.fn()}
        onOpenResource={vi.fn()}
      />,
      qc,
    )
    expect(screen.getByText('Navigate')).toBeInTheDocument()
    expect(screen.getByText('Switch cluster')).toBeInTheDocument()
  })

  it('does not search resources for a query shorter than 2 characters', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData(['resources', 'deployments', 'prod', ''], [{ name: 'web-app', namespace: 'default' }])
    const user = userEvent.setup()
    renderWithClient(
      <CommandPalette
        open={true}
        onOpenChange={vi.fn()}
        contexts={[]}
        selectedCtx="prod"
        onNavigate={vi.fn()}
        onSelectContext={vi.fn()}
        onOpenResource={vi.fn()}
      />,
      qc,
    )
    await user.type(screen.getByPlaceholderText('Type a command or search...'), 'w')
    expect(screen.queryByText('Resources')).not.toBeInTheDocument()
  })

  it('finds a match already cached from a visited resource list and opens its drawer', async () => {
    podsMock.mockResolvedValue([])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData(['resources', 'deployments', 'prod', ''], [{ name: 'web-app', namespace: 'default' }])
    const onOpenResource = vi.fn()
    const onOpenChange = vi.fn()
    const user = userEvent.setup()
    renderWithClient(
      <CommandPalette
        open={true}
        onOpenChange={onOpenChange}
        contexts={[]}
        selectedCtx="prod"
        onNavigate={vi.fn()}
        onSelectContext={vi.fn()}
        onOpenResource={onOpenResource}
      />,
      qc,
    )

    await user.type(screen.getByPlaceholderText('Type a command or search...'), 'web-a')
    expect(await screen.findByText('Resources')).toBeInTheDocument()
    const hit = screen.getByText('web-app')
    await user.click(hit)

    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'deployment', namespace: 'default', name: 'web-app' })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('ignores cached matches from a different cluster context', async () => {
    podsMock.mockResolvedValue([])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData(['resources', 'deployments', 'staging', ''], [{ name: 'web-app', namespace: 'default' }])
    const user = userEvent.setup()
    renderWithClient(
      <CommandPalette
        open={true}
        onOpenChange={vi.fn()}
        contexts={[]}
        selectedCtx="prod"
        onNavigate={vi.fn()}
        onSelectContext={vi.fn()}
        onOpenResource={vi.fn()}
      />,
      qc,
    )

    await user.type(screen.getByPlaceholderText('Type a command or search...'), 'web-a')
    await waitFor(() => expect(podsMock).toHaveBeenCalled())
    expect(screen.queryByText('Resources')).not.toBeInTheDocument()
  })

  it('also searches live pods fetched on demand', async () => {
    podsMock.mockResolvedValue([{ name: 'api-server-7f9', namespace: 'kube-system' }])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const onOpenResource = vi.fn()
    const user = userEvent.setup()
    renderWithClient(
      <CommandPalette
        open={true}
        onOpenChange={vi.fn()}
        contexts={[]}
        selectedCtx="prod"
        onNavigate={vi.fn()}
        onSelectContext={vi.fn()}
        onOpenResource={onOpenResource}
      />,
      qc,
    )

    await user.type(screen.getByPlaceholderText('Type a command or search...'), 'api-server')
    const hit = await screen.findByText('api-server-7f9')
    await user.click(hit)

    expect(onOpenResource).toHaveBeenCalledWith({ kind: 'pod', namespace: 'kube-system', name: 'api-server-7f9' })
  })
})
