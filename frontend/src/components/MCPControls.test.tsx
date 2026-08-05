import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MCPControls } from './MCPControls'
import { setAppPrefs } from '@/lib/preferences'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

// jsdom doesn't implement the Clipboard API, and userEvent.setup() installs
// its own clipboard stub as a side effect — so this has to run AFTER
// userEvent.setup(), or userEvent's stub clobbers it right back.
const writeTextMock = vi.fn().mockResolvedValue(undefined)
function stubClipboard() {
  vi.stubGlobal('navigator', { ...window.navigator, clipboard: { writeText: writeTextMock } })
}

beforeEach(() => {
  localStorage.clear()
  // preferences.ts holds its state in a module-level singleton that
  // localStorage.clear() alone doesn't reset (it only affects what a future
  // load() would read) — reset it explicitly so each test starts from a
  // known baseline regardless of what an earlier test in this file left it at.
  setAppPrefs({ mcp: { enabled: false, allowWrite: false } })
  writeTextMock.mockClear()
})

function prefs(): Record<string, unknown> {
  return JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}')
}

describe('MCPControls', () => {
  // preferences.ts holds its state in a module-level singleton, so these run
  // in file order — see ThemeToggle.test.tsx for the same constraint.
  it('renders with MCP off by default and no URL/allow-write row', () => {
    render(<MCPControls />)
    expect(screen.getByRole('switch', { name: 'Servidor MCP' })).toHaveAttribute('aria-checked', 'false')
    expect(screen.queryByTitle('Copiar')).not.toBeInTheDocument()
    expect(screen.queryByText('Permitir escrita')).not.toBeInTheDocument()
  })

  it('enabling MCP persists the choice and reveals the URL + allow-write row', async () => {
    const user = userEvent.setup()
    render(<MCPControls />)

    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: false })
    expect(screen.getByText(`${window.location.origin}/mcp`)).toBeInTheDocument()
    expect(screen.getByText('Permitir escrita')).toBeInTheDocument()
  })

  it('the copy button copies the endpoint URL', async () => {
    const user = userEvent.setup()
    render(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    stubClipboard()

    fireEvent.click(screen.getByText(`${window.location.origin}/mcp`))
    await waitFor(() => expect(writeTextMock).toHaveBeenCalledWith(`${window.location.origin}/mcp`))
  })

  it('clicking allow-write shows a confirm step before granting it', async () => {
    const user = userEvent.setup()
    render(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: false }) // not yet granted

    await user.click(screen.getByText('Cancelar'))
    expect(screen.queryByText('Confirmar')).not.toBeInTheDocument()
    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: false })

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await user.click(screen.getByText('Confirmar'))
    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: true })
  })

  it('turning MCP off clears allowWrite too', async () => {
    const user = userEvent.setup()
    render(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await user.click(screen.getByText('Confirmar'))
    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: true })

    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    expect(prefs().mcp).toEqual({ enabled: false, allowWrite: false })
  })
})
