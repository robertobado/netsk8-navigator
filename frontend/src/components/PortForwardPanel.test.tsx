import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PortForwardPanel } from './PortForwardPanel'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

const { getDetailMock, listPortForwardsMock, startPortForwardMock, stopPortForwardMock } = vi.hoisted(() => ({
  getDetailMock: vi.fn(),
  listPortForwardsMock: vi.fn(),
  startPortForwardMock: vi.fn(),
  stopPortForwardMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  getDetail: getDetailMock,
  listPortForwards: listPortForwardsMock,
  startPortForward: startPortForwardMock,
  stopPortForward: stopPortForwardMock,
}))

function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  getDetailMock.mockReset().mockResolvedValue({ ports: [{ name: 'http', port: '8080', protocol: 'TCP', extra: 'web' }] })
  listPortForwardsMock.mockReset().mockResolvedValue([])
  startPortForwardMock.mockReset().mockResolvedValue({ id: 's1', localPort: 54321 })
  stopPortForwardMock.mockReset().mockResolvedValue(undefined)
})

describe('PortForwardPanel', () => {
  it('shows a message when the pod has no container ports', async () => {
    getDetailMock.mockResolvedValue({ ports: [] })
    renderWithClient(<PortForwardPanel ctx="c" namespace="ns" name="web-1" />)
    expect(await screen.findByText('This pod exposes no container ports.')).toBeInTheDocument()
  })

  it('lists the pod ports with a Start forwarding button', async () => {
    renderWithClient(<PortForwardPanel ctx="c" namespace="ns" name="web-1" />)
    expect(await screen.findByText('8080')).toBeInTheDocument()
    expect(screen.getByText('Start forwarding')).toBeInTheDocument()
  })

  it('starts forwarding and shows the local address once the session appears', async () => {
    const user = userEvent.setup()
    listPortForwardsMock
      .mockResolvedValueOnce([]) // initial load: nothing forwarding yet
      .mockResolvedValue([{ id: 's1', namespace: 'ns', pod: 'web-1', port: 8080, localPort: 54321 }]) // after start

    renderWithClient(<PortForwardPanel ctx="c" namespace="ns" name="web-1" />)
    await user.click(await screen.findByText('Start forwarding'))

    expect(startPortForwardMock).toHaveBeenCalledWith('c', 'ns', 'web-1', 8080)
    expect(await screen.findByText('127.0.0.1:54321')).toBeInTheDocument()
    expect(screen.getByText('Stop')).toBeInTheDocument()
  })

  it('stops an active session', async () => {
    const user = userEvent.setup()
    listPortForwardsMock.mockResolvedValue([{ id: 's1', namespace: 'ns', pod: 'web-1', port: 8080, localPort: 54321 }])

    renderWithClient(<PortForwardPanel ctx="c" namespace="ns" name="web-1" />)
    await user.click(await screen.findByText('Stop'))

    await waitFor(() => expect(stopPortForwardMock).toHaveBeenCalledWith('c', 's1'))
  })
})
