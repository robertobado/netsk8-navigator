import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CRDKindView } from './CRDKindView'
import type { CRDKind } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key, tf: (_t: unknown, key: string) => key }))

const { crdListMock, drawerRkProp } = vi.hoisted(() => ({ crdListMock: vi.fn(), drawerRkProp: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, crdList: crdListMock } }
})
vi.mock('./CRDResourceDrawer', () => ({
  CRDResourceDrawer: (props: { rk: unknown; item: unknown }) => {
    drawerRkProp(props.rk)
    return props.item ? <div>drawer-open</div> : null
  },
}))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

const namespacedKind: CRDKind = { group: 'example.com', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: true, label: 'Widgets' }
const clusterScopedKind: CRDKind = {
  group: 'kwok.x-k8s.io',
  version: 'v1alpha1',
  resource: 'clusterresourceusages',
  kind: 'ClusterResourceUsage',
  namespaced: false,
  label: 'ClusterResourceUsages',
}

describe('CRDKindView', () => {
  it('shows a Namespace column for a namespaced CRD', async () => {
    crdListMock.mockResolvedValue([{ name: 'w1', namespace: 'prod', age: '1h', hosts: '', refs: '' }])
    renderWithClient(<CRDKindView ctx="c" ns="" rk={namespacedKind} />)

    expect(await screen.findByText('w1')).toBeInTheDocument()
    expect(screen.getByText('Namespace')).toBeInTheDocument()
    expect(screen.getByText('prod')).toBeInTheDocument()
  })

  it('hides the Namespace column for a cluster-scoped CRD', async () => {
    crdListMock.mockResolvedValue([{ name: 'usage-from-annotation', namespace: '', age: '1h', hosts: '', refs: '' }])
    renderWithClient(<CRDKindView ctx="c" ns="" rk={clusterScopedKind} />)

    expect(await screen.findByText('usage-from-annotation')).toBeInTheDocument()
    expect(screen.queryByText('Namespace')).not.toBeInTheDocument()
  })

  it('opens the drawer with the selected row on click', async () => {
    crdListMock.mockResolvedValue([{ name: 'w1', namespace: 'prod', age: '1h', hosts: '', refs: '' }])
    const user = userEvent.setup()
    renderWithClient(<CRDKindView ctx="c" ns="" rk={namespacedKind} />)

    await user.click(await screen.findByText('w1'))
    expect(await screen.findByText('drawer-open')).toBeInTheDocument()
    expect(drawerRkProp).toHaveBeenCalledWith(namespacedKind)
  })
})
