import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LanguageToggle } from './LanguageToggle'
import { setAppPrefs } from '@/lib/preferences'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

beforeEach(() => localStorage.clear())
// preferences.ts holds language in a module-level singleton that outlives
// this file's own tests — the "switches to en" test below would otherwise
// leave 'en' active for whichever test file runs next when the suite runs
// without per-file module isolation.
afterEach(() => setAppPrefs({ language: 'pt-BR' }))

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
