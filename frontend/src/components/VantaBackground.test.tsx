import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render } from '@testing-library/react'
import { VantaBackground } from './VantaBackground'

// Importing a real Vanta effect module registers its factory as a side effect
// on window.VANTA[NAME] (see VantaBackground.tsx) — mock that side effect so
// tests exercise the component's own mount/unmount/switch logic without
// pulling in three.js/WebGL.
const { mockFactory, destroyMock } = vi.hoisted(() => {
  const destroyMock = vi.fn()
  const mockFactory = vi.fn<(o: Record<string, unknown>) => { destroy: () => void } | null>(() => ({ destroy: destroyMock }))
  return { mockFactory, destroyMock }
})

vi.mock('three', () => ({}))
vi.mock('vanta/dist/vanta.net.min', () => {
  ;(window as unknown as { VANTA?: Record<string, unknown> }).VANTA = {
    ...(window as unknown as { VANTA?: Record<string, unknown> }).VANTA,
    NET: mockFactory,
  }
  return {}
})
vi.mock('vanta/dist/vanta.globe.min', () => {
  ;(window as unknown as { VANTA?: Record<string, unknown> }).VANTA = {
    ...(window as unknown as { VANTA?: Record<string, unknown> }).VANTA,
    GLOBE: mockFactory,
  }
  return {}
})
vi.mock('vanta/dist/vanta.waves.min', () => ({}))
vi.mock('vanta/dist/vanta.rings.min', () => ({}))
vi.mock('vanta/dist/vanta.halo.min', () => ({}))
vi.mock('vanta/dist/vanta.fog.min', () => ({}))

// Reset at the START of each test, not after — a mounted effect from the
// previous test is only unmounted (calling destroy) by vitest.setup.ts's
// afterEach(cleanup), which runs after this file's own afterEach would, so an
// afterEach-based reset here would race with that and leak a stray destroy
// call into the next test's count.
beforeEach(() => {
  vi.clearAllMocks()
})

// Flushes the microtask queue the component's async mount effect runs on
// (Promise.all([load(), import('three')]) then a couple of .then hops).
async function flush() {
  await new Promise((r) => setTimeout(r, 0))
  await new Promise((r) => setTimeout(r, 0))
}

describe('VantaBackground', () => {
  it('renders nothing and never loads an effect when disabled', async () => {
    const { container } = render(<VantaBackground enabled={false} effect="net" opacity={0.6} />)
    await flush()
    expect(container).toBeEmptyDOMElement()
    expect(mockFactory).not.toHaveBeenCalled()
  })

  it('mounts the effect with the merged base + per-effect options when enabled', async () => {
    const { container } = render(<VantaBackground enabled={true} effect="net" opacity={0.42} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(1)
    const call = mockFactory.mock.calls[0][0] as Record<string, unknown>
    expect(call.el).toBeInstanceOf(HTMLElement)
    expect(call.mouseControls).toBe(true)
    expect(call.color).toBe(0x35bec0) // net's TEAL option, merged on top of the shared base

    const div = container.querySelector('[aria-hidden]')
    expect(div).toBeInTheDocument()
    expect((div as HTMLElement).style.opacity).toBe('0.42')
  })

  it('destroys the effect on unmount', async () => {
    const { unmount } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(1)
    unmount()
    expect(destroyMock).toHaveBeenCalledTimes(1)
  })

  it('destroys the old effect and mounts the new one when the effect prop changes', async () => {
    const { rerender } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(1)

    rerender(<VantaBackground enabled={true} effect="globe" opacity={0.6} />)
    await flush()
    expect(destroyMock).toHaveBeenCalledTimes(1)
    expect(mockFactory).toHaveBeenCalledTimes(2)
    const secondCall = mockFactory.mock.calls[1][0] as Record<string, unknown>
    expect(secondCall.color2).toBe(0x326ce5) // globe's own option, absent from net's
  })

  it('tears down the effect when toggled off, and mounts again when toggled back on', async () => {
    const { rerender } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(1)

    rerender(<VantaBackground enabled={false} effect="net" opacity={0.6} />)
    await flush()
    expect(destroyMock).toHaveBeenCalledTimes(1)

    rerender(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(2)
  })

  it('degrades gracefully (no crash) when the factory throws, e.g. no WebGL context', async () => {
    mockFactory.mockImplementationOnce(() => {
      throw new Error('no webgl context')
    })
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { unmount } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(warnSpy).toHaveBeenCalledWith('Vanta background unavailable:', expect.any(Error))
    // No live effect was retained, so unmount has nothing to destroy.
    unmount()
    expect(destroyMock).not.toHaveBeenCalled()
    warnSpy.mockRestore()
  })

  it('does nothing further when the effect factory itself returns null', async () => {
    mockFactory.mockReturnValueOnce(null)
    const { unmount } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    expect(mockFactory).toHaveBeenCalledTimes(1)
    unmount()
    expect(destroyMock).not.toHaveBeenCalled()
  })

  it("lazily loads each effect kind's own chunk without crashing, even ones with no registered factory", async () => {
    const { rerender, container } = render(<VantaBackground enabled={true} effect="net" opacity={0.6} />)
    await flush()
    for (const effect of ['waves', 'rings', 'halo', 'fog'] as const) {
      rerender(<VantaBackground enabled={true} effect={effect} opacity={0.6} />)
      await flush()
    }
    expect(container.querySelector('[aria-hidden]')).toBeInTheDocument()
  })

  it('does not create an effect when window.VANTA never registered the requested factory', async () => {
    const win = window as unknown as { VANTA?: Record<string, unknown> }
    const prevGlobe = win.VANTA?.GLOBE
    if (win.VANTA) delete win.VANTA.GLOBE
    const { container } = render(<VantaBackground enabled={true} effect="globe" opacity={0.6} />)
    await flush()
    expect(mockFactory).not.toHaveBeenCalled()
    // The background layer div still renders (enabled=true) even with no live effect.
    expect(container.querySelector('[aria-hidden]')).toBeInTheDocument()
    if (win.VANTA && prevGlobe) win.VANTA.GLOBE = prevGlobe
  })
})
