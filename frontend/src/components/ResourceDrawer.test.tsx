import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { ResourceDrawer, type DrawerTarget } from './ResourceDrawer'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('./ManifestPanelLazy', () => ({ ManifestPanelLazy: (p: { kind: string; name: string }) => <div data-testid="yaml">{`yaml:${p.kind}:${p.name}`}</div> }))
vi.mock('./DetailView', () => ({
  DetailView: (p: { kind: string; name: string; onOpenResource: (t: DrawerTarget) => void }) => (
    <div data-testid="detail">
      {`detail:${p.kind}:${p.name}`}
      <button onClick={() => p.onOpenResource({ kind: 'service', namespace: 'default', name: 'web-svc' })}>drill-into-service</button>
    </div>
  ),
}))
vi.mock('./EventsPanel', () => ({ EventsPanel: (p: { kind: string; name: string }) => <div data-testid="events">{`events:${p.kind}:${p.name}`}</div> }))
vi.mock('./MultiPodLogsPanel', () => ({ MultiPodLogsPanel: (p: { kind: string; name: string }) => <div data-testid="logs">{`logs:${p.kind}:${p.name}`}</div> }))
vi.mock('./PodDrawer', () => ({ PodDrawer: (p: { pod: unknown }) => <div data-testid="pod-drawer">{p.pod ? 'open' : 'closed'}</div> }))
vi.mock('./ResourceActions', () => ({ ResourceActions: () => <div data-testid="actions" /> }))

const deployment: DrawerTarget = { kind: 'deployment', namespace: 'default', name: 'web' }

describe('ResourceDrawer', () => {
  it('renders nothing (an empty, closed drawer) when target is null', () => {
    const { container } = render(<ResourceDrawer target={null} ctx="test" onClose={vi.fn()} />)
    expect(container.querySelector('header')).toBeNull()
  })

  it('opens on the Detail tab for a kind with a detail view, and can switch tabs', () => {
    render(<ResourceDrawer target={deployment} ctx="test" onClose={vi.fn()} />)
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:deployment:web')

    fireEvent.click(screen.getByText('nav.events'))
    expect(screen.getByTestId('events')).toHaveTextContent('events:Deployment:web')

    fireEvent.click(screen.getByText('Logs'))
    expect(screen.getByTestId('logs')).toHaveTextContent('logs:deployment:web')

    fireEvent.click(screen.getByText('YAML'))
    expect(screen.getByTestId('yaml')).toHaveTextContent('yaml:deployment:web')
  })

  it('opens straight to YAML (no Details tab) for a kind with no detail view', () => {
    // Every current ManifestKind is in KINDS_WITH_DETAIL — this exercises the
    // hasDetail=false branch defensively, for whatever future kind isn't.
    const bogusKind = 'somefuturekind' as DrawerTarget['kind']
    render(<ResourceDrawer target={{ kind: bogusKind, namespace: '', name: 'thing' }} ctx="test" onClose={vi.fn()} />)
    expect(screen.queryByText('Details')).not.toBeInTheDocument()
    expect(screen.getByTestId('yaml')).toBeInTheDocument()
  })

  it('drilling into a related resource pushes it onto the in-drawer navigation stack, with a Back button', () => {
    render(<ResourceDrawer target={deployment} ctx="test" onClose={vi.fn()} />)
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:deployment:web')

    fireEvent.click(screen.getByText('drill-into-service'))
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:service:web-svc')

    fireEvent.click(screen.getByTitle('Back'))
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:deployment:web')
  })

  it('Escape pops the navigation stack before closing, then closes on the next Escape', () => {
    const onClose = vi.fn()
    render(<ResourceDrawer target={deployment} ctx="test" onClose={onClose} />)
    fireEvent.click(screen.getByText('drill-into-service'))
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:service:web-svc')

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByTestId('detail')).toHaveTextContent('detail:deployment:web')

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clicking the backdrop or the close button calls onClose', () => {
    const onClose = vi.fn()
    const { container } = render(<ResourceDrawer target={deployment} ctx="test" onClose={onClose} />)
    fireEvent.click(container.querySelector('[aria-hidden="true"]')!)
    expect(onClose).toHaveBeenCalledTimes(1)

    fireEvent.click(container.querySelectorAll('header button')[container.querySelectorAll('header button').length - 1])
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('renders a cluster-scoped resource without a namespace label crash', () => {
    render(<ResourceDrawer target={{ kind: 'node', namespace: '', name: 'node-1' }} ctx="test" onClose={vi.fn()} />)
    expect(screen.getByText('cluster-scoped')).toBeInTheDocument()
  })
})
