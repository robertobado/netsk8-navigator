import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CustomResourceView } from './CustomResourceView'
import type { RouteKind } from '@/lib/api'

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

const httpRoute: RouteKind = {
  group: 'gateway.networking.k8s.io',
  version: 'v1',
  resource: 'httproutes',
  kind: 'HTTPRoute',
  namespaced: true,
  label: 'HTTPRoutes',
  order: 0,
}

describe('CustomResourceView', () => {
  it('lists routes with hosts and gateway refs', async () => {
    crdListMock.mockResolvedValue([{ name: 'web', namespace: 'prod', age: '1h', hosts: 'web.example.com', refs: 'gw/default' }])
    renderWithClient(<CustomResourceView ctx="c" ns="" rk={httpRoute} />)

    expect(await screen.findByText('web')).toBeInTheDocument()
    expect(screen.getByText('prod')).toBeInTheDocument()
    expect(screen.getByText('web.example.com')).toBeInTheDocument()
    expect(screen.getByText('gw/default')).toBeInTheDocument()
  })

  it('shows a dash for missing hosts/refs instead of blank cells', async () => {
    crdListMock.mockResolvedValue([{ name: 'no-hosts', namespace: 'prod', age: '1h', hosts: '', refs: '' }])
    renderWithClient(<CustomResourceView ctx="c" ns="" rk={httpRoute} />)

    expect(await screen.findByText('no-hosts')).toBeInTheDocument()
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('opens the drawer with the row-shaped rk (no namespaced field) on click', async () => {
    crdListMock.mockResolvedValue([{ name: 'web', namespace: 'prod', age: '1h', hosts: '', refs: '' }])
    const user = userEvent.setup()
    renderWithClient(<CustomResourceView ctx="c" ns="" rk={httpRoute} />)

    await user.click(await screen.findByText('web'))
    expect(await screen.findByText('drawer-open')).toBeInTheDocument()
    expect(drawerRkProp).toHaveBeenCalledWith({ group: httpRoute.group, version: httpRoute.version, resource: httpRoute.resource, kind: httpRoute.kind })
  })
})
