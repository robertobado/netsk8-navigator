import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MiniGauge, UsageBasisToggle } from './UsageGauge'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

describe('MiniGauge', () => {
  it('shows — with no ceiling', () => {
    render(<MiniGauge kind="cores" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows a percentage and the absolute used value when a ceiling exists', () => {
    render(<MiniGauge g={{ used: 0.5, total: 1, unit: 'cores' }} kind="cores" />)
    expect(screen.getByText('50%')).toBeInTheDocument()
    expect(screen.getByText('0.500')).toBeInTheDocument()
  })

  it('formats bytes for the "bytes" kind', () => {
    render(<MiniGauge g={{ used: 1024, total: 2048, unit: 'bytes' }} kind="bytes" />)
    expect(screen.getByText('1.0Ki')).toBeInTheDocument()
  })
})

describe('UsageBasisToggle', () => {
  it('highlights the active basis and calls onChange', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<UsageBasisToggle basis="pct" onChange={onChange} />)
    await user.click(screen.getByText('val'))
    expect(onChange).toHaveBeenCalledWith('abs')
  })
})
