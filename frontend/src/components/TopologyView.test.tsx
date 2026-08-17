import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TopologyView } from './TopologyView'
import type { TopoGraph } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

// jsdom has no ResizeObserver — ReactFlow uses one internally to size the canvas.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

const { topologyMock } = vi.hoisted(() => ({ topologyMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, topology: topologyMock } }
})

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

const graph: TopoGraph = {
  nodes: [
    { id: 'd1', kind: 'deployment', name: 'web', status: 'Available' },
    { id: 's1', kind: 'service', name: 'web-svc', status: 'Running' },
    { id: 'p1', kind: 'pod', name: 'web-1', status: 'CrashLoopBackOff' },
  ],
  edges: [{ source: 'd1', target: 'p1' }],
}

describe('TopologyView', () => {
  it('prompts for a namespace when none is selected', () => {
    renderWithClient(<TopologyView ctx="c" ns="" />)
    expect(screen.getByText('Select a namespace at the top to view the topology.')).toBeInTheDocument()
    expect(topologyMock).not.toHaveBeenCalled()
  })

  it('shows a loading state while the graph is being fetched', () => {
    topologyMock.mockReturnValue(new Promise(() => {}))
    renderWithClient(<TopologyView ctx="c" ns="prod" />)
    expect(screen.getByText('Loading topology...')).toBeInTheDocument()
  })

  it('shows the error message when the topology query fails', async () => {
    topologyMock.mockRejectedValue(new Error('exec: no such credential helper'))
    renderWithClient(<TopologyView ctx="c" ns="prod" />)
    expect(await screen.findByText('exec: no such credential helper')).toBeInTheDocument()
  })

  it('renders a node per resource, colored/iconed by kind', async () => {
    topologyMock.mockResolvedValue(graph)
    renderWithClient(<TopologyView ctx="c" ns="prod" />)
    expect(await screen.findByText('web')).toBeInTheDocument()
    expect(screen.getByText('web-svc')).toBeInTheDocument()
    expect(screen.getByText('web-1')).toBeInTheDocument()
    expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
  })
})
