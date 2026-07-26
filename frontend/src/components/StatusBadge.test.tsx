import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatusBadge } from './StatusBadge'

describe('StatusBadge', () => {
  it.each([
    ['Running', 'ok'],
    ['Pending', 'warn'],
    ['Failed', 'err'],
    ['Unknown', 'muted'],
  ])('renders %s with the %s tone', (status) => {
    render(<StatusBadge status={status} />)
    expect(screen.getByText(status)).toBeInTheDocument()
  })

  it('is case-insensitive', () => {
    render(<StatusBadge status="RUNNING" />)
    expect(screen.getByText('RUNNING')).toBeInTheDocument()
  })
})
