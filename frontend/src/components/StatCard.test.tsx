import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Server } from 'lucide-react'
import { StatCard } from './StatCard'

describe('StatCard', () => {
  it('renders the label and value', () => {
    render(<StatCard label="Nodes" value={5} icon={Server} />)
    expect(screen.getByText('Nodes')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('shows the sub label when provided', () => {
    render(<StatCard label="Nodes" value={5} sub="4 ready" icon={Server} />)
    expect(screen.getByText('4 ready')).toBeInTheDocument()
  })

  it('hides the value and sub label while loading', () => {
    render(<StatCard label="Nodes" value={5} sub="4 ready" icon={Server} loading />)
    expect(screen.queryByText('5')).not.toBeInTheDocument()
    expect(screen.queryByText('4 ready')).not.toBeInTheDocument()
  })
})
