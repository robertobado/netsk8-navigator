import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { NamespaceSelect } from './NamespaceSelect'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const namespaces = [
  { name: 'default', status: 'Active', age: '10d' },
  { name: 'prod', status: 'Active', age: '5d' },
  { name: 'staging', status: 'Active', age: '2d' },
]

describe('NamespaceSelect', () => {
  it('shows "all namespaces" when nothing is selected', () => {
    render(<NamespaceSelect namespaces={namespaces} selected="" onSelect={vi.fn()} />)
    expect(screen.getByText('ns.all')).toBeInTheDocument()
  })

  it('opens on click and lists every namespace', async () => {
    const user = userEvent.setup()
    render(<NamespaceSelect namespaces={namespaces} selected="" onSelect={vi.fn()} />)
    await user.click(screen.getByText('ns.all'))
    expect(screen.getByText('prod')).toBeInTheDocument()
    expect(screen.getByText('staging')).toBeInTheDocument()
  })

  it('filters the list by the search query', async () => {
    const user = userEvent.setup()
    render(<NamespaceSelect namespaces={namespaces} selected="" onSelect={vi.fn()} />)
    await user.click(screen.getByText('ns.all'))
    await user.type(screen.getByPlaceholderText('ns.search'), 'sta')
    expect(screen.getByText('staging')).toBeInTheDocument()
    expect(screen.queryByText('prod')).not.toBeInTheDocument()
  })

  it('calls onSelect and closes when a namespace is picked', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<NamespaceSelect namespaces={namespaces} selected="" onSelect={onSelect} />)
    await user.click(screen.getByText('ns.all'))
    await user.click(screen.getByText('prod'))
    expect(onSelect).toHaveBeenCalledWith('prod')
    expect(screen.queryByPlaceholderText('ns.search')).not.toBeInTheDocument()
  })
})
