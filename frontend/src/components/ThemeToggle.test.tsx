import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeToggle } from './ThemeToggle'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

beforeEach(() => {
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

describe('ThemeToggle', () => {
  // preferences.ts holds its state in a module-level singleton (see
  // preferences.test.ts), so these run in file order: the default-state
  // assertion must come first, before a later test mutates it.
  it('renders one button per mode, "dark" pressed by default', () => {
    render(<ThemeToggle />)
    expect(screen.getByRole('button', { name: 'Claro' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Escuro' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Automático' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('switching to light persists the choice and updates <html data-theme>', async () => {
    const user = userEvent.setup()
    render(<ThemeToggle />)

    await user.click(screen.getByRole('button', { name: 'Claro' }))

    expect(JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}').theme).toBe('light')
    expect(screen.getByRole('button', { name: 'Claro' })).toHaveAttribute('aria-pressed', 'true')
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('switching to auto clears the <html data-theme> override', async () => {
    const user = userEvent.setup()
    render(<ThemeToggle />)

    await user.click(screen.getByRole('button', { name: 'Automático' }))

    expect(JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}').theme).toBe('auto')
    expect(document.documentElement.dataset.theme).toBeUndefined()
  })
})
