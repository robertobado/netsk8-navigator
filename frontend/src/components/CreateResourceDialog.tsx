import { useEffect, useMemo, useRef, useState } from 'react'
import Editor, { type Monaco } from '@monaco-editor/react'
import type { editor as MonacoEditor } from 'monaco-editor'
import { AlertTriangle, Loader2, Plus, X } from 'lucide-react'
import '@/lib/monaco'
import { ensureNetsk8Theme, NETSK8_THEME } from '@/lib/monacoTheme'
import { blankManifestYAML, createResource, type CreatedResource, type ManifestKind } from '@/lib/api'
import { checkYamlSyntax } from '@/lib/yaml'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

// A blank-YAML "create from scratch" dialog, reusing the same Monaco setup and
// live syntax validation as ManifestPanel's editor — this one calls Create
// instead of Update, and the kind comes from the YAML itself (not the URL), so
// pasting a manifest for a different kind than the template also works.
export function CreateResourceDialog({
  ctx,
  kind,
  namespace,
  clusterScoped,
  open,
  onClose,
  onCreated,
}: Readonly<{
  ctx: string
  kind: ManifestKind
  namespace: string
  clusterScoped: boolean
  open: boolean
  onClose: () => void
  onCreated: (result: CreatedResource) => void
}>) {
  const t = useT()
  const [value, setValue] = useState(() => blankManifestYAML(kind, namespace, clusterScoped))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const editorRef = useRef<MonacoEditor.IStandaloneCodeEditor | null>(null)
  const monacoRef = useRef<Monaco | null>(null)

  const yamlError = useMemo(() => checkYamlSyntax(value), [value])

  useEffect(() => {
    const ed = editorRef.current
    const monacoNs = monacoRef.current
    const model = ed?.getModel()
    if (!monacoNs || !model) return
    monacoNs.editor.setModelMarkers(
      model,
      'yaml-syntax',
      yamlError
        ? [
            {
              severity: monacoNs.MarkerSeverity.Error,
              message: yamlError.message,
              startLineNumber: yamlError.line,
              startColumn: yamlError.column,
              endLineNumber: yamlError.line,
              endColumn: yamlError.column + 1,
            },
          ]
        : [],
    )
  }, [yamlError])

  // Reset to a fresh template each time the dialog is (re)opened, possibly for
  // a different kind/namespace than last time.
  useEffect(() => {
    if (open) {
      setValue(blankManifestYAML(kind, namespace, clusterScoped))
      setError('')
    }
  }, [open, kind, namespace, clusterScoped])

  if (!open) return null

  const create = async () => {
    setBusy(true)
    setError('')
    try {
      const result = await createResource(ctx, value)
      onCreated(result as CreatedResource)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm">
      <div className="flex h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border bg-card shadow-2xl">
        <div className="flex items-center justify-between border-b px-5 py-3.5">
          <h2 className="text-sm font-semibold">{t('New resource')}</h2>
          <button onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
            <X className="size-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1">
          <Editor
            height="100%"
            language="yaml"
            theme={NETSK8_THEME}
            beforeMount={ensureNetsk8Theme}
            value={value}
            onChange={(v) => setValue(v ?? '')}
            onMount={(ed, monacoNs) => {
              editorRef.current = ed
              monacoRef.current = monacoNs
            }}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              lineHeight: 1.6,
              fontFamily: '"JetBrains Mono", ui-monospace, monospace',
              fontLigatures: true,
              scrollBeyondLastLine: false,
              tabSize: 2,
              renderWhitespace: 'none',
              guides: { indentation: true, highlightActiveIndentation: true },
              folding: true,
              smoothScrolling: true,
              cursorBlinking: 'smooth',
              overviewRulerLanes: 0,
              hideCursorInOverviewRuler: true,
              overviewRulerBorder: false,
              scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false },
              padding: { top: 12, bottom: 12 },
            }}
          />
        </div>
        <div className="flex items-center gap-3 border-t px-4 py-3">
          <button
            onClick={create}
            disabled={busy || !!yamlError}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
              !yamlError ? 'bg-primary text-primary-foreground hover:opacity-90' : 'cursor-not-allowed bg-muted text-muted-foreground',
              busy && 'opacity-50',
            )}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
            {t('Create')}
          </button>
          <button onClick={onClose} className="rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground">
            {t('Cancel')}
          </button>
          {yamlError ? (
            <span className="flex items-center gap-1.5 truncate text-xs text-[color:var(--err)]">
              <AlertTriangle className="size-3.5 shrink-0" />
              {t('Line')} {yamlError.line}: {yamlError.message}
            </span>
          ) : (
            error && <span className="truncate text-xs text-[color:var(--err)]">{error}</span>
          )}
        </div>
      </div>
    </div>
  )
}
