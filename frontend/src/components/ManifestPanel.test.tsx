import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ManifestPanel } from './ManifestPanel'
import type { ResourceRef } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))
vi.mock('@/lib/monaco', () => ({}))
vi.mock('@/lib/monacoTheme', () => ({ NETSK8_THEME: 'test-theme', ensureNetsk8Theme: vi.fn() }))

// Monaco needs real workers/DOM APIs jsdom doesn't provide — stand in with a
// plain textarea (Editor) and a before/after readout (DiffEditor), which is
// all this component's own logic (dirty state, preview/apply flow) depends on.
vi.mock('@monaco-editor/react', () => ({
  default: ({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: { readOnly: boolean } }) => (
    <textarea aria-label="yaml" value={value} readOnly={options.readOnly} onChange={(e) => onChange(e.target.value)} />
  ),
  DiffEditor: ({ original, modified }: { original: string; modified: string }) => (
    <div data-testid="diff">
      <div data-testid="diff-original">{original}</div>
      <div data-testid="diff-modified">{modified}</div>
    </div>
  ),
}))

const { getManifestRefMock, applyManifestRefMock } = vi.hoisted(() => ({
  getManifestRefMock: vi.fn(),
  applyManifestRefMock: vi.fn(),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, getManifestRef: getManifestRefMock, applyManifestRef: applyManifestRefMock }
})

const writeTextMock = vi.fn().mockResolvedValue(undefined)
const ref = { kind: 'deployment', gvr: undefined } as unknown as ResourceRef

function renderPanel(editable = true) {
  return render(<ManifestPanel ctx="test" kind={ref} namespace="default" name="web" editable={editable} />)
}

beforeEach(() => {
  vi.stubGlobal('navigator', { ...window.navigator, clipboard: { writeText: writeTextMock } })
})

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('ManifestPanel', () => {
  it('shows a loading state, then the fetched manifest', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\nmetadata:\n  name: web\n')
    renderPanel()
    expect(screen.getByText('Loading manifest...')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText('yaml')).toHaveValue('kind: Deployment\nmetadata:\n  name: web\n'))
    expect(getManifestRefMock).toHaveBeenCalledWith('test', ref, 'default', 'web')
  })

  it('shows an error state when the fetch fails', async () => {
    getManifestRefMock.mockRejectedValue(new Error('not found'))
    renderPanel()
    expect(await screen.findByText('not found')).toBeInTheDocument()
  })

  it('read-only mode: no footer, no syntax checking, editor is read-only', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    renderPanel(false)
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()
    expect(screen.getByText(/read-only/)).toBeInTheDocument()
    expect(screen.getByLabelText('yaml')).toHaveAttribute('readonly')
    expect(screen.queryByText('Preview')).not.toBeInTheDocument()
  })

  it('Preview runs a dry-run apply and shows the diff; Confirm apply then applies for real', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\nspec:\n  replicas: 1\n')
    applyManifestRefMock.mockResolvedValue('kind: Deployment\nspec:\n  replicas: 2\n')
    renderPanel()
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('yaml'), { target: { value: 'kind: Deployment\nspec:\n  replicas: 2\n' } })
    expect(screen.getByText('Discard')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Preview'))
    expect(await screen.findByTestId('diff-modified')).toHaveTextContent('replicas: 2')
    expect(applyManifestRefMock).toHaveBeenCalledWith('test', ref, 'default', 'web', 'kind: Deployment\nspec:\n  replicas: 2\n', { dryRun: true })
    expect(screen.getByText('Apply to the live cluster?')).toBeInTheDocument()

    applyManifestRefMock.mockResolvedValue(undefined)
    vi.useFakeTimers() // enabled before the click so the revert setTimeout(…, 3000) it schedules is fake
    fireEvent.click(screen.getByText('Confirm apply'))
    // findBy/waitFor poll on a (now-fake) timer, so flush the pending apply
    // promise directly instead — same reason vi.advanceTimersByTime is used
    // below rather than a real-timer-based query.
    await act(async () => {})
    expect(applyManifestRefMock).toHaveBeenLastCalledWith('test', ref, 'default', 'web', 'kind: Deployment\nspec:\n  replicas: 2\n')
    expect(screen.getByText('Applied to cluster')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(screen.queryByText('Applied to cluster')).not.toBeInTheDocument()
  })

  it('Back to edit returns from the diff view without applying', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    applyManifestRefMock.mockResolvedValue('kind: Deployment\n')
    renderPanel()
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('yaml'), { target: { value: 'kind: Deployment\nextra: true\n' } })

    fireEvent.click(screen.getByText('Preview'))
    expect(await screen.findByText('Back to edit')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Back to edit'))
    expect(applyManifestRefMock).toHaveBeenCalledTimes(1) // the dry run only — no apply
    expect(screen.getByLabelText('yaml')).toBeInTheDocument()
  })

  it('a preview failure surfaces the error instead of entering confirm mode', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    applyManifestRefMock.mockRejectedValue(new Error('admission webhook denied the request'))
    renderPanel()
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('yaml'), { target: { value: 'kind: Deployment\nextra: true\n' } })

    fireEvent.click(screen.getByText('Preview'))
    expect(await screen.findByText('admission webhook denied the request')).toBeInTheDocument()
    expect(screen.queryByText('Apply to the live cluster?')).not.toBeInTheDocument()
  })

  it('Discard reverts unsaved edits back to the loaded manifest', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    renderPanel()
    await waitFor(() => expect(screen.getByLabelText('yaml')).toHaveValue('kind: Deployment\n'))
    fireEvent.change(screen.getByLabelText('yaml'), { target: { value: 'kind: Deployment\nextra: true\n' } })
    fireEvent.click(screen.getByText('Discard'))
    expect(screen.getByLabelText('yaml')).toHaveValue('kind: Deployment\n')
  })

  it('invalid YAML disables Preview and shows a line/column error instead of the dry-run error', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    renderPanel()
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('yaml'), { target: { value: 'kind: [unterminated' } })
    expect(screen.getByText(/^Line \d+:/)).toBeInTheDocument()
    expect(screen.getByText('Preview').closest('button')).toBeDisabled()
  })

  it('Copy writes the current YAML to the clipboard and shows Copied briefly', async () => {
    getManifestRefMock.mockResolvedValue('kind: Deployment\n')
    renderPanel()
    expect(await screen.findByLabelText('yaml')).toBeInTheDocument()

    vi.useFakeTimers() // enabled before the click so the revert setTimeout(…, 1500) it schedules is fake
    fireEvent.click(screen.getByTitle('Copy YAML'))
    await act(async () => {}) // flush the pending clipboard-write promise (see the Confirm apply case above)
    expect(writeTextMock).toHaveBeenCalledWith('kind: Deployment\n')
    expect(screen.getByText('Copied')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(1500)
    })
    expect(screen.getByText('Copy')).toBeInTheDocument()
  })
})
