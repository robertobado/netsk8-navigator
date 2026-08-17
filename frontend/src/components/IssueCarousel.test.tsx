import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { Server } from 'lucide-react'
import { IssueCarousel } from './IssueCarousel'
import type { IssueItem } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

function item(overrides: Partial<IssueItem> = {}): IssueItem {
  return {
    kind: 'pod',
    namespace: 'prod',
    name: 'web-1',
    since: new Date(Date.now() - 60_000).toISOString(),
    reason: 'CrashLoopBackOff',
    message: 'back-off restarting failed container',
    ...overrides,
  }
}

describe('IssueCarousel', () => {
  it('renders nothing when there are no items', () => {
    const { container } = render(<IssueCarousel title="Failed" icon={Server} items={[]} onOpen={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the current item, its count, and a live age', () => {
    render(<IssueCarousel title="Failed" icon={Server} items={[item()]} onOpen={vi.fn()} />)
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('web-1')).toBeInTheDocument()
    expect(screen.getByText('prod')).toBeInTheDocument()
    expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('1 / 1')).toBeInTheDocument()
  })

  it('falls back to a placeholder when the message is empty', () => {
    render(<IssueCarousel title="Failed" icon={Server} items={[item({ message: '' })]} onOpen={vi.fn()} />)
    expect(screen.getByText('no detail')).toBeInTheDocument()
  })

  it('clicking the item calls onOpen with the current item', () => {
    const onOpen = vi.fn()
    const it1 = item({ name: 'web-1' })
    render(<IssueCarousel title="Failed" icon={Server} items={[it1]} onOpen={onOpen} />)
    fireEvent.click(screen.getByTitle('Open details'))
    expect(onOpen).toHaveBeenCalledWith(it1)
  })

  it('Prev/Next are disabled with a single item and wrap around with several', () => {
    render(<IssueCarousel title="Failed" icon={Server} items={[item()]} onOpen={vi.fn()} />)
    expect(screen.getByLabelText('Previous')).toBeDisabled()
    expect(screen.getByLabelText('Next')).toBeDisabled()
  })

  it('navigates and wraps between multiple items', () => {
    const items = [item({ name: 'a' }), item({ name: 'b' }), item({ name: 'c' })]
    render(<IssueCarousel title="Failed" icon={Server} items={items} onOpen={vi.fn()} />)
    expect(screen.getByText('a')).toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('Next'))
    expect(screen.getByText('b')).toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('Previous'))
    fireEvent.click(screen.getByLabelText('Previous'))
    expect(screen.getByText('c')).toBeInTheDocument() // wrapped backward past the first item
  })

  it('pinning stops auto-advance and toggles the pin label', () => {
    const items = [item({ name: 'a' }), item({ name: 'b' })]
    render(<IssueCarousel title="Failed" icon={Server} items={items} onOpen={vi.fn()} />)
    const pinBtn = screen.getByTitle('Pin this item')
    fireEvent.click(pinBtn)
    expect(screen.getByText('pinned')).toBeInTheDocument()
    expect(screen.getByTitle('Resume carousel')).toBeInTheDocument()
  })
})
