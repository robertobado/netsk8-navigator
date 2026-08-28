import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MCPControls } from './MCPControls'
import { setAppPrefs } from '@/lib/preferences'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

const { mcpTokenMock, contextsMock, healthMock, regenerateMCPTokenMock } = vi.hoisted(() => ({
  mcpTokenMock: vi.fn(),
  contextsMock: vi.fn(),
  healthMock: vi.fn(),
  regenerateMCPTokenMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: { mcpToken: mcpTokenMock, contexts: contextsMock, health: healthMock },
  regenerateMCPToken: regenerateMCPTokenMock,
}))

// jsdom doesn't implement the Clipboard API, and userEvent.setup() installs
// its own clipboard stub as a side effect — so this has to run AFTER
// userEvent.setup(), or userEvent's stub clobbers it right back.
const writeTextMock = vi.fn().mockResolvedValue(undefined)
function stubClipboard() {
  vi.stubGlobal('navigator', { ...window.navigator, clipboard: { writeText: writeTextMock } })
}

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  localStorage.clear()
  // preferences.ts holds its state in a module-level singleton that
  // localStorage.clear() alone doesn't reset (it only affects what a future
  // load() would read) — reset it explicitly so each test starts from a
  // known baseline regardless of what an earlier test in this file left it at.
  setAppPrefs({ mcp: { enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] } })
  writeTextMock.mockClear()
  mcpTokenMock.mockReset().mockResolvedValue({ token: 'abcdef1234567890token' })
  contextsMock.mockReset().mockResolvedValue([])
  healthMock.mockReset().mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: 'test', authEnabled: true })
  regenerateMCPTokenMock.mockReset().mockResolvedValue({ token: 'freshfreshfreshtoken0' })
})

function prefs(): Record<string, unknown> {
  return JSON.parse(localStorage.getItem('netsk8.prefs') ?? '{}')
}

describe('MCPControls', () => {
  // preferences.ts holds its state in a module-level singleton, so these run
  // in file order — see ThemeToggle.test.tsx for the same constraint.
  it('renders with MCP off by default and no URL/allow-write row', () => {
    renderWithClient(<MCPControls />)
    expect(screen.getByRole('switch', { name: 'Servidor MCP' })).toHaveAttribute('aria-checked', 'false')
    expect(screen.queryByTitle('Copiar')).not.toBeInTheDocument()
    expect(screen.queryByText('Permitir escrita')).not.toBeInTheDocument()
  })

  it('enabling MCP persists the choice and reveals the URL + token + allow-write row', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)

    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    expect(prefs().mcp).toEqual({ enabled: true, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] })
    expect(screen.getByText(`${window.location.origin}/mcp`)).toBeInTheDocument()
    expect(screen.getByText('Permitir escrita')).toBeInTheDocument()
    await waitFor(() => expect(mcpTokenMock).toHaveBeenCalled())
    expect(await screen.findByText('Token de acesso')).toBeInTheDocument()
  })

  it('the copy button copies the endpoint URL', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    stubClipboard()

    fireEvent.click(screen.getByText(`${window.location.origin}/mcp`))
    await waitFor(() => expect(writeTextMock).toHaveBeenCalledWith(`${window.location.origin}/mcp`))
  })

  it('the token is masked by default and reveals on click', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    await screen.findByText('Token de acesso')

    expect(screen.queryByText('abcdef1234567890token')).not.toBeInTheDocument()
    await user.click(screen.getByTitle('Revelar'))
    expect(screen.getByText('abcdef1234567890token')).toBeInTheDocument()
  })

  it('regenerating the token requires confirmation and refetches it', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    await screen.findByText('Token de acesso')

    await user.click(screen.getByTitle('Gerar novo token'))
    await user.click(screen.getByText('Confirmar'))

    await waitFor(() => expect(regenerateMCPTokenMock).toHaveBeenCalled())
    await waitFor(() => expect(mcpTokenMock).toHaveBeenCalledTimes(2)) // initial load + post-regenerate refetch
  })

  it('clicking allow-write shows a confirm step before granting it', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    expect(prefs().mcp).toMatchObject({ enabled: true, allowWrite: false }) // not yet granted

    await user.click(screen.getByText('Cancelar'))
    expect(screen.queryByText('Confirmar')).not.toBeInTheDocument()
    expect(prefs().mcp).toMatchObject({ enabled: true, allowWrite: false })

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await user.click(screen.getByText('Confirmar'))
    expect(prefs().mcp).toMatchObject({ enabled: true, allowWrite: true })
  })

  it('warns when granting write access with AUTH_PASSWORD unset', async () => {
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: 'test', authEnabled: false })
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    expect(await screen.findByText(/AUTH_PASSWORD não está definido/)).toBeInTheDocument()
  })

  it('does not warn when granting write access with AUTH_PASSWORD set', async () => {
    healthMock.mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: 'test', authEnabled: true })
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await waitFor(() => expect(healthMock).toHaveBeenCalled())
    expect(screen.queryByText(/AUTH_PASSWORD não está definido/)).not.toBeInTheDocument()
  })

  it('turning MCP off clears allowWrite too', async () => {
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await user.click(screen.getByText('Confirmar'))
    expect(prefs().mcp).toMatchObject({ enabled: true, allowWrite: true })

    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))

    expect(prefs().mcp).toMatchObject({ enabled: false, allowWrite: false })
  })

  it('pins a context read-only via the add picker, and unpins it via its chip', async () => {
    contextsMock.mockResolvedValue([
      { name: 'staging', cluster: 'staging', user: 'staging', namespace: 'default', server: '', current: false },
      { name: 'prod', cluster: 'prod', user: 'prod', namespace: 'default', server: '', current: false },
    ])
    const user = userEvent.setup()
    renderWithClient(<MCPControls />)
    await user.click(screen.getByRole('switch', { name: 'Servidor MCP' }))
    await user.click(screen.getByRole('switch', { name: 'Permitir escrita' }))
    await user.click(screen.getByText('Confirmar'))
    await waitFor(() => expect(contextsMock).toHaveBeenCalled())

    // Nothing pinned yet — no chips, only the "add" affordance.
    expect(screen.queryByText('prod')).not.toBeInTheDocument()

    await user.click(screen.getByText('Fixar contexto como somente leitura'))
    await user.click(await screen.findByRole('button', { name: 'prod' }))

    expect((prefs().mcp as { readOnlyContexts: string[] }).readOnlyContexts).toEqual(['prod'])
    expect(screen.getByText('prod')).toBeInTheDocument()
    // Already-pinned contexts drop out of the still-open picker's list.
    expect(screen.queryByRole('button', { name: 'prod' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'staging' })).toBeInTheDocument()

    await user.click(screen.getByLabelText('Remover prod'))
    expect((prefs().mcp as { readOnlyContexts: string[] }).readOnlyContexts).toEqual([])
    expect(screen.queryByText('prod')).not.toBeInTheDocument()
  })
})
