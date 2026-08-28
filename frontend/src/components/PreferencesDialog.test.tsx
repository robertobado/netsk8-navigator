import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PreferencesDialog } from './PreferencesDialog'
import { setAppPrefs } from '@/lib/preferences'
import type { VantaSettings } from '@/lib/vanta'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

const { mcpTokenMock, contextsMock, healthMock } = vi.hoisted(() => ({
  mcpTokenMock: vi.fn(),
  contextsMock: vi.fn(),
  healthMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: { mcpToken: mcpTokenMock, contexts: contextsMock, health: healthMock },
  regenerateMCPToken: vi.fn(),
}))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

function vantaProps(overrides: Partial<VantaSettings> = {}): VantaSettings {
  return {
    enabled: false,
    effect: 'net',
    opacity: 0.6,
    setEnabled: vi.fn(),
    setEffect: vi.fn(),
    setOpacity: vi.fn(),
    ...overrides,
  }
}

beforeEach(() => {
  localStorage.clear()
  setAppPrefs({ mcp: { enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] } })
  mcpTokenMock.mockReset().mockResolvedValue({ token: 'abcdef1234567890token' })
  contextsMock.mockReset().mockResolvedValue([])
  healthMock.mockReset().mockResolvedValue({ status: 'ok', kubeconfig: '', demo: false, version: 'test', authEnabled: true })
})

describe('PreferencesDialog', () => {
  it('renders nothing when closed', () => {
    renderWithClient(<PreferencesDialog open={false} onClose={vi.fn()} vanta={vantaProps()} />)
    expect(screen.queryByText('Preferências')).not.toBeInTheDocument()
  })

  it('shows all four preference controls when open', () => {
    renderWithClient(<PreferencesDialog open={true} onClose={vi.fn()} vanta={vantaProps()} />)
    expect(screen.getByText('Preferências')).toBeInTheDocument()
    expect(screen.getByText('Fundo animado')).toBeInTheDocument()
    expect(screen.getByText('Servidor MCP')).toBeInTheDocument()
    expect(screen.getByText('Tema')).toBeInTheDocument()
    expect(screen.getByText('Idioma')).toBeInTheDocument()
  })

  it('calls onClose when the X button is clicked', async () => {
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<PreferencesDialog open={true} onClose={onClose} vanta={vantaProps()} />)
    await user.click(screen.getByLabelText('Fechar'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on Escape', () => {
    const onClose = vi.fn()
    renderWithClient(<PreferencesDialog open={true} onClose={onClose} vanta={vantaProps()} />)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on a backdrop click but not on a click inside the panel', () => {
    const onClose = vi.fn()
    const { container } = renderWithClient(<PreferencesDialog open={true} onClose={onClose} vanta={vantaProps()} />)
    fireEvent.click(screen.getByText('Preferências'))
    expect(onClose).not.toHaveBeenCalled()
    // The backdrop is a separate aria-hidden sibling, not an ancestor of the
    // panel (see PreferencesDialog.tsx) — select it directly.
    fireEvent.click(container.querySelector('[aria-hidden="true"]')!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
