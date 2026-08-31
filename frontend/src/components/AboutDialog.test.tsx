import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { AboutDialog } from './AboutDialog'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AboutDialog', () => {
  it('renders nothing when closed', () => {
    const { container } = render(<AboutDialog open={false} onClose={vi.fn()} version="1.2.3" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the app name and version when open', () => {
    render(<AboutDialog open={true} onClose={vi.fn()} version="1.2.3" />)
    expect(screen.getByRole('heading', { name: 'Netsk8 Navigator' })).toBeInTheDocument()
    expect(screen.getByText('v1.2.3')).toBeInTheDocument()
  })

  it('labels a "dev" version as a development build instead of "vdev"', () => {
    render(<AboutDialog open={true} onClose={vi.fn()} version="dev" />)
    expect(screen.getByText('about.devBuild')).toBeInTheDocument()
    expect(screen.queryByText('vdev')).not.toBeInTheDocument()
  })

  it('shows an update link when a newer version is available', () => {
    render(
      <AboutDialog open={true} onClose={vi.fn()} version="1.2.3" update={{ available: true, latest: '1.3.0', url: 'https://example.com/releases/1.3.0' }} />,
    )
    const link = screen.getByRole('link', { name: /update.available1.3.0/ })
    expect(link).toHaveAttribute('href', 'https://example.com/releases/1.3.0')
    expect(screen.queryByText('about.upToDate')).not.toBeInTheDocument()
  })

  it('falls back to the releases page when an available update has no url', () => {
    render(<AboutDialog open={true} onClose={vi.fn()} version="1.2.3" update={{ available: true, latest: '1.3.0' }} />)
    expect(screen.getByRole('link', { name: /update.available1.3.0/ })).toHaveAttribute(
      'href',
      'https://github.com/robertobado/netsk8-navigator/releases/latest',
    )
  })

  it('shows "up to date" when no update is available', () => {
    render(<AboutDialog open={true} onClose={vi.fn()} version="1.2.3" update={{ available: false }} />)
    expect(screen.getByText('about.upToDate')).toBeInTheDocument()
  })

  it('links to the GitHub repo', () => {
    render(<AboutDialog open={true} onClose={vi.fn()} version="1.2.3" />)
    expect(screen.getByRole('link', { name: /about.viewOnGithub/ })).toHaveAttribute('href', 'https://github.com/robertobado/netsk8-navigator')
  })

  // target="_blank" alone does nothing in the Wails desktop app, and that
  // app's window never has Wails' own JS bridge either (see openExternal's
  // doc comment in lib/utils.ts) — both external links go through
  // POST /api/open-external instead.
  it('opens the GitHub link via POST /api/open-external', () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    render(<AboutDialog open={true} onClose={vi.fn()} version="1.2.3" />)

    fireEvent.click(screen.getByRole('link', { name: /about.viewOnGithub/ }))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/open-external',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ url: 'https://github.com/robertobado/netsk8-navigator' }) }),
    )
  })

  it('opens the update link via POST /api/open-external', () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AboutDialog open={true} onClose={vi.fn()} version="1.2.3" update={{ available: true, latest: '1.3.0', url: 'https://example.com/releases/1.3.0' }} />,
    )

    fireEvent.click(screen.getByRole('link', { name: /update.available1.3.0/ }))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/open-external',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ url: 'https://example.com/releases/1.3.0' }) }),
    )
  })

  it('calls onClose when the X button is clicked', () => {
    const onClose = vi.fn()
    render(<AboutDialog open={true} onClose={onClose} version="1.2.3" />)
    fireEvent.click(screen.getByLabelText('Close'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on Escape', () => {
    const onClose = vi.fn()
    render(<AboutDialog open={true} onClose={onClose} version="1.2.3" />)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on a backdrop click but not on a click inside the panel', () => {
    const onClose = vi.fn()
    const { container } = render(<AboutDialog open={true} onClose={onClose} version="1.2.3" />)
    fireEvent.click(screen.getByRole('heading', { name: 'Netsk8 Navigator' }))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(container.querySelector('[aria-hidden="true"]')!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
