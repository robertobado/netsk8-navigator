import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { execURL } from '@/lib/api'

// Interactive shell into a pod container: xterm.js <-> WebSocket <-> SPDY exec.
export function TerminalPanel({ ctx, namespace, pod, container }: { ctx: string; namespace: string; pod: string; container?: string }) {
  const host = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!host.current) return

    const term = new Terminal({
      fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace',
      fontSize: 13,
      cursorBlink: true,
      theme: {
        background: '#0b0e14',
        foreground: '#d6dae3',
        cursor: '#8b8cf0',
        selectionBackground: '#2a2f42',
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host.current)
    fit.fit()

    const ws = new WebSocket(execURL(ctx, namespace, pod, container))
    ws.binaryType = 'arraybuffer'

    const sendResize = () => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }
    }

    ws.onopen = () => {
      term.writeln('\x1b[90mConectando ao container...\x1b[0m')
      sendResize()
      term.focus()
    }
    ws.onmessage = (e) => {
      const text = typeof e.data === 'string' ? e.data : new TextDecoder().decode(e.data)
      term.write(text)
    }
    ws.onclose = () => term.writeln('\r\n\x1b[90m[sessão encerrada]\x1b[0m')
    ws.onerror = () => term.writeln('\r\n\x1b[31m[erro de conexão]\x1b[0m')

    const dataSub = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'stdin', data }))
    })

    const onResize = () => {
      fit.fit()
      sendResize()
    }
    window.addEventListener('resize', onResize)
    // Fit again on next tick once the drawer finished its open transition.
    const t = setTimeout(onResize, 60)

    return () => {
      clearTimeout(t)
      window.removeEventListener('resize', onResize)
      dataSub.dispose()
      ws.close()
      term.dispose()
    }
  }, [ctx, namespace, pod, container])

  return <div ref={host} className="h-full w-full bg-[#0b0e14] p-2" />
}
