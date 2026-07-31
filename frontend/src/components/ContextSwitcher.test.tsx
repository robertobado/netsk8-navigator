import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ContextSwitcher } from './ContextSwitcher'
import type { ContextInfo } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

// `current` reflects the kubeconfig's static default context, which can
// differ from `selected` — the context actually active in this app session
// (e.g. after the user picks a different one from this very list).
const contexts: ContextInfo[] = [
  { name: 'prod', cluster: 'prod', user: 'prod', namespace: 'default', server: 'https://prod', current: true },
  { name: 'staging', cluster: 'staging', user: 'staging', namespace: 'default', server: 'https://staging', current: false },
]

function openDropdown(user: ReturnType<typeof userEvent.setup>) {
  // Only the toggle button exists before the list opens.
  return user.click(screen.getAllByRole('button')[0])
}

describe('ContextSwitcher', () => {
  it('puts the "current" badge on the selected context, not the kubeconfig default', async () => {
    const user = userEvent.setup()
    render(<ContextSwitcher contexts={contexts} selected="staging" onSelect={vi.fn()} />)
    await openDropdown(user)

    const rows = screen.getAllByRole('button').slice(1)
    const stagingRow = rows.find((r) => r.textContent?.includes('staging'))
    const prodRow = rows.find((r) => r.textContent?.includes('prod'))

    expect(stagingRow?.textContent).toContain('current')
    expect(prodRow?.textContent).not.toContain('current')
  })

  it('calls onSelect with the clicked context name', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<ContextSwitcher contexts={contexts} selected="prod" onSelect={onSelect} />)
    await openDropdown(user)

    const rows = screen.getAllByRole('button').slice(1)
    const stagingRow = rows.find((r) => r.textContent?.includes('staging'))!
    await user.click(stagingRow)

    expect(onSelect).toHaveBeenCalledWith('staging')
  })
})
