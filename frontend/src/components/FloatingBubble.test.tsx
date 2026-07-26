import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FloatingBubble } from './FloatingBubble'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('FloatingBubble', () => {
  it('renders the message linking to href', () => {
    render(<FloatingBubble message="Check us out on GitHub" href="https://github.com/robertobado/netsk8-navigator" />)
    const link = screen.getByRole('link', { name: /check us out on github/i })
    expect(link).toHaveAttribute('href', 'https://github.com/robertobado/netsk8-navigator')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('disappears once dismissed', async () => {
    const user = userEvent.setup()
    render(<FloatingBubble message="hello" href="https://example.com" />)
    expect(screen.getByText('hello')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(screen.queryByText('hello')).not.toBeInTheDocument()
  })

  it('renders parked in place, without animating, when the user prefers reduced motion', () => {
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    render(<FloatingBubble message="hello" href="https://example.com" />)
    const link = screen.getByRole('link', { name: /hello/i })
    // Parked via an inline transform (not the rAF loop that drives the
    // normal drifting/fleeing behavior) — respects prefers-reduced-motion.
    expect(link.parentElement?.getAttribute('style')).toContain('calc')
  })
})
