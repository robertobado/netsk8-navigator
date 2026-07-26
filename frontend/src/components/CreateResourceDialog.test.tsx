import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CreateResourceDialog } from './CreateResourceDialog'

// Identity translator — decouples these tests from i18n dictionary content.
vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

// Monaco needs real workers/DOM APIs jsdom doesn't provide — stand in with a
// plain textarea wired the same way (value/onChange), which is all this
// component's own logic (validation, dirty state) actually depends on.
vi.mock('@monaco-editor/react', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea aria-label="yaml" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}))
vi.mock('@/lib/monaco', () => ({}))
vi.mock('@/lib/monacoTheme', () => ({ NETSK8_THEME: 'test-theme', ensureNetsk8Theme: vi.fn() }))

const { createResourceMock } = vi.hoisted(() => ({ createResourceMock: vi.fn() }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, createResource: createResourceMock }
})

describe('CreateResourceDialog', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <CreateResourceDialog ctx="c" kind="configmap" namespace="prod" clusterScoped={false} open={false} onClose={vi.fn()} onCreated={vi.fn()} />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('seeds the editor with a blank template for the given kind/namespace', () => {
    render(<CreateResourceDialog ctx="c" kind="deployment" namespace="prod" clusterScoped={false} open={true} onClose={vi.fn()} onCreated={vi.fn()} />)
    const editor = screen.getByLabelText('yaml') as HTMLTextAreaElement
    expect(editor.value).toContain('kind: Deployment')
    expect(editor.value).toContain('namespace: prod')
  })

  it('disables Create while the YAML is syntactically broken', async () => {
    const user = userEvent.setup()
    render(<CreateResourceDialog ctx="c" kind="configmap" namespace="prod" clusterScoped={false} open={true} onClose={vi.fn()} onCreated={vi.fn()} />)
    const editor = screen.getByLabelText('yaml')
    await user.clear(editor)
    await user.type(editor, 'foo: bar\n  bad: nested')
    expect(screen.getByText('Create')).toBeDisabled()
  })

  it('creates the resource and reports the result', async () => {
    createResourceMock.mockResolvedValue({ status: 'created', kind: 'ConfigMap', namespace: 'prod', name: 'cfg' })
    const onCreated = vi.fn()
    const user = userEvent.setup()
    render(<CreateResourceDialog ctx="c" kind="configmap" namespace="prod" clusterScoped={false} open={true} onClose={vi.fn()} onCreated={onCreated} />)

    await user.click(screen.getByText('Create'))

    await waitFor(() => expect(createResourceMock).toHaveBeenCalled())
    expect(createResourceMock.mock.calls[0][0]).toBe('c')
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith({ status: 'created', kind: 'ConfigMap', namespace: 'prod', name: 'cfg' }))
  })

  it('shows the backend error message on failure', async () => {
    createResourceMock.mockRejectedValue(new Error('metadata.name is required'))
    const user = userEvent.setup()
    render(<CreateResourceDialog ctx="c" kind="configmap" namespace="prod" clusterScoped={false} open={true} onClose={vi.fn()} onCreated={vi.fn()} />)

    await user.click(screen.getByText('Create'))

    expect(await screen.findByText('metadata.name is required')).toBeInTheDocument()
  })

  it('calls onClose when Cancel is clicked', async () => {
    const onClose = vi.fn()
    const user = userEvent.setup()
    render(<CreateResourceDialog ctx="c" kind="configmap" namespace="prod" clusterScoped={false} open={true} onClose={onClose} onCreated={vi.fn()} />)
    await user.click(screen.getByText('Cancel'))
    expect(onClose).toHaveBeenCalled()
  })
})
