import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Switch } from './Switch'

describe('Switch', () => {
  it('reflects checked via aria-checked and calls onChange when clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Switch checked={false} onChange={onChange} label="Example" />)

    const el = screen.getByRole('switch', { name: 'Example' })
    expect(el).toHaveAttribute('aria-checked', 'false')
    await user.click(el)
    expect(onChange).toHaveBeenCalledTimes(1)
  })

  it('renders checked state with the default active class', () => {
    render(<Switch checked={true} onChange={vi.fn()} label="Example" />)
    const el = screen.getByRole('switch', { name: 'Example' })
    expect(el).toHaveAttribute('aria-checked', 'true')
    expect(el.className).toContain('bg-[color:var(--brand)]')
  })

  it('accepts a custom activeClassName', () => {
    render(<Switch checked={true} onChange={vi.fn()} label="Example" activeClassName="bg-[color:var(--err)]" />)
    const el = screen.getByRole('switch', { name: 'Example' })
    expect(el.className).toContain('bg-[color:var(--err)]')
  })

  it('is disabled and non-interactive when disabled is set', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Switch checked={false} onChange={onChange} label="Example" disabled />)
    const el = screen.getByRole('switch', { name: 'Example' })
    expect(el).toBeDisabled()
    await user.click(el)
    expect(onChange).not.toHaveBeenCalled()
  })
})
