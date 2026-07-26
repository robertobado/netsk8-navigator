import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LanguageToggle } from './LanguageToggle'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

beforeEach(() => localStorage.clear())

describe('LanguageToggle', () => {
  it('renders both language options', () => {
    render(<LanguageToggle />)
    expect(screen.getByText('PT')).toBeInTheDocument()
    expect(screen.getByText('EN')).toBeInTheDocument()
  })

  it('switches the active language on click', async () => {
    const user = userEvent.setup()
    render(<LanguageToggle />)
    await user.click(screen.getByText('EN'))
    expect(JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}').language).toBe('en')
  })
})
