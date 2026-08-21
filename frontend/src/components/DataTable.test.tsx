import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
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

  it('collapses an expanded row on a second click', async () => {
    const user = userEvent.setup()
    render(<DataTable title="Pods" data={data} columns={columns} expandable={(row) => (row.name === 'web-1' ? <div>Extra detail</div> : null)} />)
    const firstRow = screen.getAllByRole('row')[1]
    await user.click(firstRow)
    expect(screen.getByText('Extra detail')).toBeInTheDocument()
    await user.click(firstRow)
    expect(screen.queryByText('Extra detail')).not.toBeInTheDocument()
  })

  it('renders extra content below a row via renderSubRow', () => {
    render(<DataTable title="Pods" data={data} columns={columns} renderSubRow={(row) => <span>sub for {row.name}</span>} />)
    expect(screen.getByText('sub for web-1')).toBeInTheDocument()
    expect(screen.getByText('sub for web-2')).toBeInTheDocument()
  })

  describe('sort persistence', () => {
    afterEach(() => localStorage.clear())

    it('restores a previously persisted sort order', () => {
      localStorage.setItem('netsk8.sort.pods-key', JSON.stringify([{ id: 'name', desc: true }]))
      render(<DataTable title="Pods" data={data} columns={columns} storageKey="pods-key" />)
      const rowsInOrder = screen
        .getAllByRole('row')
        .slice(1)
        .map((r) => r.textContent)
      expect(rowsInOrder[0]).toContain('web-2')
    })

    it('falls back to unsorted when the persisted value is not valid JSON', () => {
      localStorage.setItem('netsk8.sort.pods-key', '{not-json')
      render(<DataTable title="Pods" data={data} columns={columns} storageKey="pods-key" />)
      const rowsInOrder = screen
        .getAllByRole('row')
        .slice(1)
        .map((r) => r.textContent)
      expect(rowsInOrder[0]).toContain('web-1')
    })

    it('persists the sort order to localStorage when it changes', async () => {
      const user = userEvent.setup()
      render(<DataTable title="Pods" data={data} columns={columns} storageKey="pods-key" />)
      await user.click(screen.getByText('Name'))
      expect(localStorage.getItem('netsk8.sort.pods-key')).toBe(JSON.stringify([{ id: 'name', desc: false }]))
    })
  })

  describe('facet filter', () => {
    it('filters rows by a selected facet value and clears it again', async () => {
      const user = userEvent.setup()
      render(<DataTable title="Pods" data={data} columns={columns} facets={['status']} />)

      await user.click(screen.getByTitle('Filter column'))
      expect(screen.getByRole('button', { name: 'Running' })).toBeInTheDocument()
      const pendingOption = screen.getByRole('button', { name: 'Pending' })

      await user.click(pendingOption)
      expect(screen.queryByText('web-1')).not.toBeInTheDocument()
      expect(screen.getByText('web-2')).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Clear filter' }))
      expect(screen.getByText('web-1')).toBeInTheDocument()
      expect(screen.getByText('web-2')).toBeInTheDocument()
    })

    it('toggling the same facet value off restores the row', async () => {
      const user = userEvent.setup()
      render(<DataTable title="Pods" data={data} columns={columns} facets={['status']} />)
      await user.click(screen.getByTitle('Filter column'))
      await user.click(screen.getByRole('button', { name: 'Pending' }))
      expect(screen.queryByText('web-1')).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Pending' }))
      expect(screen.getByText('web-1')).toBeInTheDocument()
    })

    it('closes the dropdown when clicking outside it', async () => {
      const user = userEvent.setup()
      const { container } = render(<DataTable title="Pods" data={data} columns={columns} facets={['status']} />)
      await user.click(screen.getByTitle('Filter column'))
      expect(screen.getByRole('button', { name: 'Running' })).toBeInTheDocument()
      const overlay = container.querySelector('[aria-hidden="true"].fixed.inset-0') as HTMLElement
      await user.click(overlay)
      expect(screen.queryByRole('button', { name: 'Running' })).not.toBeInTheDocument()
    })

    it('shows "No values" when the faceted column has nothing to filter on', async () => {
      const emptyData: Row[] = [
        { name: 'web-1', status: '' },
        { name: 'web-2', status: '' },
      ]
      const user = userEvent.setup()
      render(<DataTable title="Pods" data={emptyData} columns={columns} facets={['status']} />)
      await user.click(screen.getByTitle('Filter column'))
      expect(screen.getByText('No values')).toBeInTheDocument()
    })
  })

  describe('virtualization', () => {
    const manyRows: Row[] = Array.from({ length: 120 }, (_, i) => ({ name: `web-${i}`, status: i % 2 === 0 ? 'Running' : 'Pending' }))

    // jsdom never does real layout, so the scroll container's offsetHeight is
    // always 0 — the virtualizer would then compute an empty visible range.
    // Give it a plausible viewport height so it renders an actual window of rows.
    let offsetHeightSpy: ReturnType<typeof vi.spyOn>
    beforeEach(() => {
      offsetHeightSpy = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(600)
    })
    afterEach(() => offsetHeightSpy.mockRestore())

    it('renders every row natively when virtualize is not set, even with many rows', () => {
      render(<DataTable title="Pods" data={manyRows} columns={columns} />)
      expect(screen.getByText('web-0')).toBeInTheDocument()
      expect(screen.getByText('web-119')).toBeInTheDocument()
    })

    it('windows rows when virtualize is set and the row count is above the threshold', () => {
      render(<DataTable title="Pods" data={manyRows} columns={columns} virtualize />)
      // First rows are still mounted (top of the window)...
      expect(screen.getByText('web-0')).toBeInTheDocument()
      // ...but far-off rows outside the overscan window are not mounted.
      expect(screen.queryByText('web-119')).not.toBeInTheDocument()
    })

    it('stays unvirtualized below the row-count threshold even when virtualize is set', () => {
      render(<DataTable title="Pods" data={data} columns={columns} virtualize />)
      expect(screen.getByText('web-1')).toBeInTheDocument()
      expect(screen.getByText('web-2')).toBeInTheDocument()
    })
  })
})
