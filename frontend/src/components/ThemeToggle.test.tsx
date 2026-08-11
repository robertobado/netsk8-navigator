import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeToggle } from './ThemeToggle'
import { setAppPrefs } from '@/lib/preferences'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

beforeEach(() => {
  localStorage.clear()
  delete document.documentElement.dataset.theme
  // preferences.ts holds theme in a module-level singleton that
  // localStorage.clear() alone doesn't reset — pin it back to the default
  // explicitly so these tests don't inherit whatever another test file
  // left it at when the suite runs without per-file module isolation.
  setAppPrefs({ theme: 'dark' })
})

describe('ThemeToggle', () => {
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
