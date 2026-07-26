import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HoverBubble } from './HoverBubble'

describe('HoverBubble', () => {
  it('shows the content on hover and hides it on mouse leave', async () => {
    const user = userEvent.setup()
    render(
      <HoverBubble content="Extra info">
        <span>trigger</span>
      </HoverBubble>,
    )
    expect(screen.queryByText('Extra info')).not.toBeInTheDocument()

    await user.hover(screen.getByText('trigger'))
    expect(screen.getByText('Extra info')).toBeInTheDocument()

    await user.unhover(screen.getByText('trigger'))
    expect(screen.queryByText('Extra info')).not.toBeInTheDocument()
  })
})
