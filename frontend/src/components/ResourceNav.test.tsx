import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ResourceNav } from './ResourceNav'
import type { CRDKind } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const certManagerKinds: CRDKind[] = [
  { group: 'cert-manager.io', version: 'v1', resource: 'certificates', kind: 'Certificate', namespaced: true, label: 'Certificates' },
  { group: 'cert-manager.io', version: 'v1', resource: 'issuers', kind: 'Issuer', namespaced: true, label: 'Issuers' },
]
const kwokKinds: CRDKind[] = [{ group: 'kwok.x-k8s.io', version: 'v1alpha1', resource: 'logs', kind: 'Logs', namespaced: true, label: 'Logs' }]

describe('ResourceNav — Custom Resources tree', () => {
  it('renders no Custom Resources section when the cluster has no CRDs', () => {
    render(<ResourceNav active="overview" onSelect={vi.fn()} crdKinds={[]} />)
    expect(screen.queryByText('group.customResources')).not.toBeInTheDocument()
  })

  it('chunks CRDs by API group, collapsed by default, with a count per group', () => {
    render(<ResourceNav active="overview" onSelect={vi.fn()} crdKinds={[...certManagerKinds, ...kwokKinds]} />)

    expect(screen.getByText('group.customResources')).toBeInTheDocument()
    expect(screen.getByText('cert-manager.io')).toBeInTheDocument()
    expect(screen.getByText('kwok.x-k8s.io')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument() // cert-manager.io has 2 kinds
    expect(screen.getByText('1')).toBeInTheDocument() // kwok.x-k8s.io has 1 kind

    // Collapsed by default — no kind names visible yet.
    expect(screen.queryByText('Certificates')).not.toBeInTheDocument()
    expect(screen.queryByText('Logs')).not.toBeInTheDocument()
  })

  it('expands a group on click and selects a kind from it', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<ResourceNav active="overview" onSelect={onSelect} crdKinds={certManagerKinds} />)

    await user.click(screen.getByText('cert-manager.io'))
    expect(await screen.findByText('Certificates')).toBeInTheDocument()
    expect(screen.getByText('Issuers')).toBeInTheDocument()

    await user.click(screen.getByText('Certificates'))
    expect(onSelect).toHaveBeenCalledWith('crdkind:cert-manager.io/v1/certificates')
  })

  it('auto-expands the group containing the active view', () => {
    render(<ResourceNav active="crdkind:kwok.x-k8s.io/v1alpha1/logs" onSelect={vi.fn()} crdKinds={[...certManagerKinds, ...kwokKinds]} />)

    // kwok.x-k8s.io's only kind is the active one — its group starts expanded.
    expect(screen.getByText('Logs')).toBeInTheDocument()
    // cert-manager.io isn't active — stays collapsed.
    expect(screen.queryByText('Certificates')).not.toBeInTheDocument()
  })
})
