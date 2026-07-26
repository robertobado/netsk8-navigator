import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ServiceAccountExpansion } from './ResourceExpansions'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key, tf: (key: string) => key }))

const { serviceAccountUsageMock } = vi.hoisted(() => ({ serviceAccountUsageMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, serviceAccountUsage: serviceAccountUsageMock } }
})

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  serviceAccountUsageMock.mockReset()
})

describe('ServiceAccountExpansion', () => {
  it('renders the effective permissions unioned from every bound role', async () => {
    serviceAccountUsageMock.mockResolvedValue({
      bindings: [{ kind: 'RoleBinding', slug: 'rolebinding', namespace: 'prod', name: 'web-pod-reader' }],
      pods: [],
      permissions: [
        { label: 'get,list', value: 'core/pods' },
        { label: 'get', value: 'core/nodes' },
      ],
    })
    renderWithClient(<ServiceAccountExpansion ctx="c" namespace="prod" name="web" onOpen={vi.fn()} />)

    expect(await screen.findByText('get,list')).toBeInTheDocument()
    expect(screen.getByText('core/pods')).toBeInTheDocument()
    expect(screen.getByText('get')).toBeInTheDocument()
    expect(screen.getByText('core/nodes')).toBeInTheDocument()
  })

  it('shows no permissions section when the SA has no bindings', async () => {
    serviceAccountUsageMock.mockResolvedValue({ bindings: [], pods: [], permissions: [] })
    renderWithClient(<ServiceAccountExpansion ctx="c" namespace="prod" name="orphan" onOpen={vi.fn()} />)

    expect(await screen.findByText('No binding references this SA and no pod uses it.')).toBeInTheDocument()
    expect(screen.queryByText('Effective permissions')).not.toBeInTheDocument()
  })
})
