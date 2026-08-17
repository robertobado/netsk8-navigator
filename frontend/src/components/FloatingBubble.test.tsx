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

  // Without reduced motion, the mount effect picks a random off-screen spawn
  // side (above/right/below/left) and starts an rAF loop. Math.random is
  // stubbed per case so every branch of that spawn-side switch actually runs.
  it.each([
    [0, 'above'],
    [0.3, 'right'],
    [0.6, 'below'],
    [0.9, 'left'],
  ])('spawns and animates at least one frame without crashing (random=%s, %s side)', (randomValue) => {
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    vi.spyOn(Math, 'random').mockReturnValue(randomValue)
    // Only run the tick loop's body once — it re-schedules itself via another
    // requestAnimationFrame call, which would otherwise recurse synchronously
    // forever under a mock that always invokes immediately.
    let calls = 0
    const rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      calls++
      if (calls === 1) cb(performance.now())
      return calls
    })
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {})

    const { unmount } = render(<FloatingBubble message="hello" href="https://example.com" />)
    expect(screen.getByRole('link', { name: /hello/i }).parentElement).toHaveAttribute('style', expect.stringContaining('translate3d'))

    unmount()
    rafSpy.mockRestore()
    vi.restoreAllMocks()
  })
})
