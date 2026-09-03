import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ContextSwitcher } from './ContextSwitcher'
import type { ContextInfo } from '@/lib/api'
import { setAppPrefs } from '@/lib/preferences'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string, fallback?: string) => fallback ?? key }))

beforeEach(() => {
  localStorage.clear()
  setAppPrefs({ contexts: { favorites: [] } })
})

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

  it('sorts favorited contexts to the top', async () => {
    setAppPrefs({ contexts: { favorites: ['staging'] } })
    const user = userEvent.setup()
    render(<ContextSwitcher contexts={contexts} selected="prod" onSelect={vi.fn()} />)
    await openDropdown(user)

    // Slice off the outer toggle button — with selected="prod" its own
    // label already contains "prod", which would otherwise shadow the
    // list row being looked for below.
    const names = screen
      .getAllByRole('button')
      .slice(1)
      .map((b) => b.getAttribute('aria-label') ?? b.textContent ?? '')
    // 'staging' (favorited) must appear before 'prod' despite alphabetical order putting prod first.
    const stagingIdx = names.findIndex((n) => n.includes('staging'))
    const prodIdx = names.findIndex((n) => n.includes('prod'))
    expect(stagingIdx).toBeGreaterThan(-1)
    expect(stagingIdx).toBeLessThan(prodIdx)
  })

  it('shows a "Manage kubeconfig" entry below the list that fires onManageKubeconfig and closes the dropdown', async () => {
    const user = userEvent.setup()
    const onManageKubeconfig = vi.fn()
    const onSelect = vi.fn()
    render(<ContextSwitcher contexts={contexts} selected="prod" onSelect={onSelect} onManageKubeconfig={onManageKubeconfig} />)
    await openDropdown(user)

    const manage = screen.getByRole('button', { name: 'Manage kubeconfig' })
    // It sits after every context row, not among them.
    const buttons = screen.getAllByRole('button')
    expect(buttons.indexOf(manage)).toBe(buttons.length - 1)

    await user.click(manage)
    expect(onManageKubeconfig).toHaveBeenCalledOnce()
    expect(onSelect).not.toHaveBeenCalled()
    // Dropdown closed — the search box is gone.
    expect(screen.queryByPlaceholderText('Search cluster...')).not.toBeInTheDocument()
  })

  it('omits the "Manage kubeconfig" entry when onManageKubeconfig is not provided', async () => {
    const user = userEvent.setup()
    render(<ContextSwitcher contexts={contexts} selected="prod" onSelect={vi.fn()} />)
    await openDropdown(user)

    expect(screen.queryByRole('button', { name: 'Manage kubeconfig' })).not.toBeInTheDocument()
  })

  it('toggles a context as favorite without triggering onSelect', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<ContextSwitcher contexts={contexts} selected="prod" onSelect={onSelect} />)
    await openDropdown(user)

    const prodSelectButton = screen
      .getAllByRole('button')
      .slice(1)
      .find((b) => b.textContent?.includes('prod'))!
    const prodStarButton = prodSelectButton.nextElementSibling as HTMLElement

    await user.click(prodStarButton)
    expect(onSelect).not.toHaveBeenCalled()
    expect(JSON.parse(localStorage.getItem('netsk8.prefs')!).contexts.favorites).toContain('prod')
    expect(prodStarButton).toHaveAttribute('aria-label', 'Remove favorite')

    await user.click(prodStarButton)
    expect(JSON.parse(localStorage.getItem('netsk8.prefs')!).contexts.favorites).not.toContain('prod')
  })
})
