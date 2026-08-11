import { describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/react'
import { NavigatorLoader, LoaderPreview } from './Loader'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

describe('NavigatorLoader', () => {
  it('renders with defaults (connecting, navy sky, no label)', () => {
    const { container } = render(<NavigatorLoader />)
    expect(container.querySelector('svg[role="img"]')).toBeInTheDocument()
    expect(container.querySelector('.nk-label')).toBeNull()
  })

  it('renders the ready state, green sky, custom size, and a label', () => {
    const { container } = render(<NavigatorLoader size={64} state="ready" sky="green" label="Connecting…" />)
    expect(container.querySelector('svg')).toHaveAttribute('width', '64')
    expect(container.querySelector('.nk--ready')).toBeInTheDocument()
    expect(container.querySelector('.nk-label')).toHaveTextContent('Connecting…')
  })
})

describe('LoaderPreview', () => {
  it('renders every size/sky combination without throwing', () => {
    const { container } = render(<LoaderPreview />)
    expect(container.querySelectorAll('svg').length).toBeGreaterThanOrEqual(5)
  })
})
