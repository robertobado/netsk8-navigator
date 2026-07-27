import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createColumnHelper, type ColumnDef } from '@tanstack/react-table'
import { DataTable } from './DataTable'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

interface Row {
  name: string
  status: string
}
const col = createColumnHelper<Row>()
const columns = [col.accessor('name', { header: 'Name' }), col.accessor('status', { header: 'Status' })] as ColumnDef<Row, unknown>[]
const data: Row[] = [
  { name: 'web-1', status: 'Running' },
  { name: 'web-2', status: 'Pending' },
]

describe('DataTable', () => {
  it('renders the title, row count, and rows', () => {
    render(<DataTable title="Pods" data={data} columns={columns} />)
    expect(screen.getByText('Pods')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByText('web-1')).toBeInTheDocument()
    expect(screen.getByText('web-2')).toBeInTheDocument()
  })

  it('shows a loading state when there are no rows yet', () => {
    render(<DataTable title="Pods" data={[]} columns={columns} loading />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('shows an empty label when there is no data and it is not loading', () => {
    render(<DataTable title="Pods" data={[]} columns={columns} emptyLabel="No pods found" />)
    expect(screen.getByText('No pods found')).toBeInTheDocument()
  })

  it('filters rows via the search input', async () => {
    const user = userEvent.setup()
    render(<DataTable title="Pods" data={data} columns={columns} />)
    await user.type(screen.getByPlaceholderText('Filter...'), 'web-2')
    expect(screen.queryByText('web-1')).not.toBeInTheDocument()
    expect(screen.getByText('web-2')).toBeInTheDocument()
  })

  it('calls onRowClick when a row is clicked', async () => {
    const user = userEvent.setup()
    const onRowClick = vi.fn()
    render(<DataTable title="Pods" data={data} columns={columns} onRowClick={onRowClick} />)
    await user.click(screen.getByText('web-1'))
    expect(onRowClick).toHaveBeenCalledWith(data[0])
  })

  it('toggles sorting when a sortable header is clicked', async () => {
    const user = userEvent.setup()
    render(<DataTable title="Pods" data={data} columns={columns} />)
    const rowsInOrder = () =>
      screen
        .getAllByRole('row')
        .slice(1)
        .map((r) => r.textContent)
    expect(rowsInOrder()[0]).toContain('web-1')
    await user.click(screen.getByText('Name'))
    // Ascending by name is already web-1,web-2 — a second click flips to descending.
    await user.click(screen.getByText('Name'))
    expect(rowsInOrder()[0]).toContain('web-2')
  })

  it('expands a row when it is expandable', async () => {
    const user = userEvent.setup()
    render(<DataTable title="Pods" data={data} columns={columns} expandable={(row) => (row.name === 'web-1' ? <div>Extra detail</div> : null)} />)
    expect(screen.queryByText('Extra detail')).not.toBeInTheDocument()
    // The chevron cell is the first cell of the expandable row.
    const firstRow = screen.getAllByRole('row')[1]
    await user.click(firstRow)
    expect(screen.getByText('Extra detail')).toBeInTheDocument()
  })
})
