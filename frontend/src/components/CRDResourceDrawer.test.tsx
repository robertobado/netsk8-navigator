import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CRDResourceDrawer } from './CRDResourceDrawer'
import type { CRDRef } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('./EventsPanel', () => ({ EventsPanel: () => null }))

const { manifestPanelKindProp, resourceActionsKindProp } = vi.hoisted(() => ({
  manifestPanelKindProp: vi.fn(),
  resourceActionsKindProp: vi.fn(),
}))
vi.mock('./ManifestPanelLazy', () => ({
  ManifestPanelLazy: (props: { kind: unknown }) => {
    manifestPanelKindProp(props.kind)
    return <div>yaml-panel</div>
  },
}))
vi.mock('./ResourceActions', () => ({
  ResourceActions: (props: { kind: unknown }) => {
    resourceActionsKindProp(props.kind)
    return <div>resource-actions</div>
  },
}))

const { crdDetailMock } = vi.hoisted(() => ({ crdDetailMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, crdDetail: crdDetailMock }
})

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

const rk: CRDRef & { kind: string } = { group: 'example.com', version: 'v1', resource: 'widgets', kind: 'Widget' }

describe('CRDResourceDrawer', () => {
  it('passes a CRDRef (not a ManifestKind string) to ManifestPanel and ResourceActions', async () => {
    crdDetailMock.mockResolvedValue({ kind: 'Widget', name: 'w1', namespace: 'prod', age: '1h' })
    const user = userEvent.setup()
    renderWithClient(<CRDResourceDrawer ctx="c" rk={rk} item={{ name: 'w1', namespace: 'prod' }} onClose={() => {}} />)

    expect(resourceActionsKindProp).toHaveBeenCalledWith(rk)

    await user.click(screen.getByText('YAML'))
    expect(manifestPanelKindProp).toHaveBeenCalledWith(rk)
  })

  it('shows nothing when there is no selected item', () => {
    const { container } = renderWithClient(<CRDResourceDrawer ctx="c" rk={rk} item={null} onClose={() => {}} />)
    expect(container.querySelector('header')).not.toBeInTheDocument()
  })
})
