import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/react'
import { TerminalPanel } from './TerminalPanel'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

// @xterm/xterm does real canvas/DOM rendering jsdom can't do — stub it (and the
// fit addon) with plain spies so this file exercises TerminalPanel's own
// connection lifecycle (WS <-> terminal wiring) instead of xterm internals.
const { FakeTerminal, FakeFitAddon } = vi.hoisted(() => {
  class FakeTerminal {
    static instances: FakeTerminal[] = []
    cols = 80
    rows = 24
    writeln = vi.fn()
    write = vi.fn()
    focus = vi.fn()
    open = vi.fn()
    loadAddon = vi.fn()
    dispose = vi.fn()
    onDataCb: ((data: string) => void) | null = null
    disposeDataSub = vi.fn()
    constructor() {
      FakeTerminal.instances.push(this)
    }
    onData(cb: (data: string) => void) {
      this.onDataCb = cb
      return { dispose: this.disposeDataSub }
    }
  }
  class FakeFitAddon {
    static instances: FakeFitAddon[] = []
    fit = vi.fn()
    constructor() {
      FakeFitAddon.instances.push(this)
    }
  }
  return { FakeTerminal, FakeFitAddon }
})
vi.mock('@xterm/xterm', () => ({ Terminal: FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: FakeFitAddon }))
vi.mock('@xterm/xterm/css/xterm.css', () => ({}))

// jsdom has no WebSocket — stand in with a controllable fake, same idea as
// MultiPodLogsPanel.test.tsx's FakeEventSource for EventSource.
class FakeWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  static instances: FakeWebSocket[] = []
  url: string
  readyState = FakeWebSocket.CONNECTING
  binaryType = ''
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closeSpy = vi.fn()
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.closeSpy()
    this.readyState = FakeWebSocket.CLOSED
  }
}

beforeEach(() => {
  FakeTerminal.instances = []
  FakeFitAddon.instances = []
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

function open(ws: FakeWebSocket) {
  ws.readyState = FakeWebSocket.OPEN
  ws.onopen?.()
}

describe('TerminalPanel', () => {
  it('opens a websocket to the pod exec endpoint', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" container="app" />)
    expect(FakeWebSocket.instances).toHaveLength(1)
    const ws = FakeWebSocket.instances[0]
    expect(ws.url).toBe('ws://localhost/api/contexts/c/pods/prod/web-1/exec?container=app')
    expect(ws.binaryType).toBe('arraybuffer')
  })

  it('greets, resizes, and focuses the terminal once the socket opens', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    open(ws)

    expect(term.writeln).toHaveBeenCalledWith(expect.stringContaining('Connecting to container...'))
    expect(ws.sent).toEqual([JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })])
    expect(term.focus).toHaveBeenCalled()
  })

  it('does not send a resize before the socket is open', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    // still CONNECTING — resize must not be sent yet
    expect(ws.sent).toEqual([])
  })

  it('writes string messages straight to the terminal', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    ws.onmessage?.({ data: 'hello from the pod' })
    expect(term.write).toHaveBeenCalledWith('hello from the pod')
  })

  it('decodes binary messages before writing them to the terminal', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    const buf = new TextEncoder().encode('binary hello').buffer
    ws.onmessage?.({ data: buf })
    expect(term.write).toHaveBeenCalledWith('binary hello')
  })

  it('shows a session-closed message when the socket closes', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    ws.onclose?.()
    expect(term.writeln).toHaveBeenCalledWith(expect.stringContaining('session closed'))
  })

  it('shows a connection-error message on socket error', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    ws.onerror?.()
    expect(term.writeln).toHaveBeenCalledWith(expect.stringContaining('connection error'))
  })

  it('forwards terminal keystrokes to the socket only while it is open', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]

    term.onDataCb?.('ls\n')
    expect(ws.sent).toEqual([])

    open(ws)
    term.onDataCb?.('ls\n')
    expect(ws.sent).toContain(JSON.stringify({ type: 'stdin', data: 'ls\n' }))
  })

  it('re-fits and re-sends the terminal size on window resize', () => {
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const fit = FakeFitAddon.instances[0]
    open(ws)
    const fitCallsAfterOpen = fit.fit.mock.calls.length

    window.dispatchEvent(new Event('resize'))
    expect(fit.fit.mock.calls.length).toBeGreaterThan(fitCallsAfterOpen)
    expect(ws.sent.length).toBeGreaterThan(1)
  })

  it('fits again shortly after mount, once the drawer transition finishes', () => {
    vi.useFakeTimers()
    render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const fit = FakeFitAddon.instances[0]
    const fitCallsAtMount = fit.fit.mock.calls.length

    vi.advanceTimersByTime(60)
    expect(fit.fit.mock.calls.length).toBeGreaterThan(fitCallsAtMount)
  })

  it('tears down the socket, terminal, and listeners on unmount', () => {
    const { unmount } = render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" />)
    const ws = FakeWebSocket.instances[0]
    const term = FakeTerminal.instances[0]
    const removeSpy = vi.spyOn(window, 'removeEventListener')

    unmount()

    expect(term.disposeDataSub).toHaveBeenCalled()
    expect(ws.closeSpy).toHaveBeenCalled()
    expect(term.dispose).toHaveBeenCalled()
    expect(removeSpy).toHaveBeenCalledWith('resize', expect.any(Function))
  })

  it('reconnects with a fresh socket when the target pod/container changes', () => {
    const { rerender } = render(<TerminalPanel ctx="c" namespace="prod" pod="web-1" container="app" />)
    expect(FakeWebSocket.instances).toHaveLength(1)

    rerender(<TerminalPanel ctx="c" namespace="prod" pod="web-2" container="app" />)
    expect(FakeWebSocket.instances).toHaveLength(2)
    expect(FakeWebSocket.instances[1].url).toBe('ws://localhost/api/contexts/c/pods/prod/web-2/exec?container=app')
    // the previous socket/terminal were torn down as part of the reconnect
    expect(FakeWebSocket.instances[0].closeSpy).toHaveBeenCalled()
    expect(FakeTerminal.instances[0].dispose).toHaveBeenCalled()
  })
})
