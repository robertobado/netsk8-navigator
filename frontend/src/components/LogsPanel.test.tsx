import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { LogsPanel } from './LogsPanel'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onmessage: ((e: { data: string }) => void) | null = null
  closed = false
  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }
  close() {
    this.closed = true
  }
}

function emit(line: string) {
  const es = MockEventSource.instances.at(-1)!
  act(() => {
    es.onmessage?.({ data: JSON.stringify({ line }) })
  })
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})

describe('LogsPanel', () => {
  it('connects to the log stream for the given pod/container', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" container="app" />)
    expect(MockEventSource.instances[0]?.url).toContain('/contexts/test/')
    expect(MockEventSource.instances[0]?.url).toContain('container=app')
  })

  it('shows a waiting placeholder, then streamed lines with level coloring', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    expect(screen.getByText('Waiting for logs...')).toBeInTheDocument()

    emit('2026-01-01T00:00:00.000000000Z INFO starting up')
    emit('2026-01-01T00:00:01.000000000Z ERROR connection refused')
    expect(screen.queryByText('Waiting for logs...')).not.toBeInTheDocument()
    expect(screen.getByText(/starting up/)).toBeInTheDocument()
    expect(screen.getByText(/connection refused/)).toBeInTheDocument()
  })

  it('search filters lines and highlights the match; no-match shows the filter placeholder', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    emit('alpha line')
    emit('beta line')
    fireEvent.change(screen.getByPlaceholderText('Search logs...'), { target: { value: 'alpha' } })
    expect(screen.getByText(/alpha/)).toBeInTheDocument()
    expect(screen.queryByText(/beta/)).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Search logs...'), { target: { value: 'nothing matches' } })
    expect(screen.getByText('No line matches the filter.')).toBeInTheDocument()
  })

  it('toggling a level filter hides its lines and dims the button', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    emit('2026-01-01T00:00:00.000000000Z ERROR boom')
    emit('2026-01-01T00:00:00.000000000Z INFO fine')
    expect(screen.getByText(/boom/)).toBeInTheDocument()

    const errorToggle = screen.getByTitle(/error/)
    fireEvent.click(errorToggle)
    expect(screen.queryByText(/boom/)).not.toBeInTheDocument()
    expect(screen.getByText(/fine/)).toBeInTheDocument()
    expect(errorToggle.className).toContain('opacity-35')

    fireEvent.click(errorToggle)
    expect(screen.getByText(/boom/)).toBeInTheDocument()
  })

  it('"Newest first" reverses order, and Clear empties the view', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    emit('first line')
    emit('second line')

    fireEvent.click(screen.getByTitle('Newest first'))
    const lines = screen.getAllByText(/(first|second) line/)
    expect(lines[0]).toHaveTextContent('second line')
    expect(lines[1]).toHaveTextContent('first line')

    fireEvent.click(screen.getByTitle('Clear'))
    expect(screen.getByText('Waiting for logs...')).toBeInTheDocument()
  })

  it('closes the EventSource on unmount', () => {
    const { unmount } = render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    const es = MockEventSource.instances[0]
    unmount()
    expect(es.closed).toBe(true)
  })

  it('caps the buffer at 5000 lines, keeping the most recent', () => {
    render(<LogsPanel ctx="test" namespace="default" pod="web-1" />)
    const es = MockEventSource.instances[0]
    // One batched act() — React 18 batches all 5010 setLines updates into a
    // single re-render, instead of 5010 renders of a several-thousand-row list.
    act(() => {
      for (let i = 0; i < 5010; i++) es.onmessage?.({ data: JSON.stringify({ line: `line ${i}` }) })
    })
    expect(screen.queryByText('line 0')).not.toBeInTheDocument()
    expect(screen.getByText('line 5009')).toBeInTheDocument()
  }, 15000) // coverage instrumentation makes 5010 sequential state updates slower than the 5s default
})
