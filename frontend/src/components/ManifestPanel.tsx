import { useEffect, useMemo, useRef, useState } from 'react'
import Editor, { DiffEditor, type Monaco } from '@monaco-editor/react'
import type { editor as MonacoEditor } from 'monaco-editor'
import { AlertTriangle, Check, Copy, Loader2, Lock, RotateCcw, Save } from 'lucide-react'
import '@/lib/monaco'
import { ensureNetsk8Theme, NETSK8_THEME } from '@/lib/monacoTheme'
import { applyManifest, getManifest, type ManifestKind } from '@/lib/api'
import { cn } from '@/lib/utils'
import { checkYamlSyntax } from '@/lib/yaml'
import { useT } from '@/lib/i18n'

type State = 'loading' | 'ready' | 'error'

// Monaco-based YAML viewer/editor for a resource manifest. Editing mutates the
// live cluster, so applying goes through a server-side dry-run first: the
// frontend previews a diff of what the API server would actually produce
// (including its own defaulting) and surfaces validation errors before
// anything is committed for real.
export function ManifestPanel({
  ctx,
  kind,
  namespace,
  name,
  editable,
}: {
  ctx: string
  kind: ManifestKind
  namespace: string
  name: string
  editable: boolean
}) {
  const t = useT()
  const [state, setState] = useState<State>('loading')
  const [error, setError] = useState('')
  const [value, setValue] = useState('')
  const original = useRef('')
  // `confirming` = a dry-run succeeded and we're showing its diff, waiting on
  // the user to confirm the real (non-dry-run) apply.
  const [confirming, setConfirming] = useState(false)
  const [previewYaml, setPreviewYaml] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const [applying, setApplying] = useState(false)
  const [applied, setApplied] = useState(false)
  const [copied, setCopied] = useState(false)
  const editorRef = useRef<MonacoEditor.IStandaloneCodeEditor | null>(null)
  const monacoRef = useRef<Monaco | null>(null)

  // Client-side syntax check, live as the user types — separate from the
  // server-side dry-run validation (which also catches things like an
  // immutable field change, not just malformed YAML).
  const yamlError = useMemo(() => (editable ? checkYamlSyntax(value) : null), [value, editable])

  // Underline the exact spot in the editor, same as Monaco's own diagnostics.
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

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard blocked — ignore */
    }
  }

  useEffect(() => {
    let cancelled = false
    setState('loading')
    setConfirming(false)
    setApplied(false)
    getManifest(ctx, kind, namespace, name)
      .then((yaml) => {
        if (cancelled) return
        original.current = yaml
        setValue(yaml)
        setState('ready')
      })
      .catch((e) => {
        if (cancelled) return
        setError((e as Error).message)
        setState('error')
      })
    return () => {
      cancelled = true
    }
  }, [ctx, kind, namespace, name])

  const dirty = value !== original.current

  const preview = async () => {
    setPreviewing(true)
    setError('')
    try {
      const result = await applyManifest(ctx, kind, namespace, name, value, { dryRun: true })
      setPreviewYaml(result ?? value)
      setConfirming(true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setPreviewing(false)
    }
  }

  const apply = async () => {
    setApplying(true)
    setError('')
    try {
      await applyManifest(ctx, kind, namespace, name, value)
      original.current = value
      setApplied(true)
      setConfirming(false)
      setTimeout(() => setApplied(false), 3000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setApplying(false)
    }
  }

  if (state === 'loading')
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> {t('Loading manifest...')}
      </div>
    )
  if (state === 'error') return <div className="flex h-full items-center justify-center p-6 text-center text-sm text-[color:var(--err)]">{error}</div>

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-1.5">
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          {!editable && <Lock className="size-3" />}
          {confirming ? t('Reviewing changes') : editable ? 'YAML' : `YAML · ${t('read-only')}`}
        </span>
        {!confirming && (
          <button
            onClick={copy}
            className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            title={t('Copy YAML')}
          >
            {copied ? <Check className="size-3.5 text-[color:var(--ok)]" /> : <Copy className="size-3.5" />}
            {copied ? t('Copied') : t('Copy')}
          </button>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {confirming ? (
          <DiffEditor
            height="100%"
            language="yaml"
            theme={NETSK8_THEME}
            beforeMount={ensureNetsk8Theme}
            original={original.current}
            modified={previewYaml}
            options={{
              readOnly: true,
              renderSideBySide: true,
              minimap: { enabled: false },
              fontSize: 13,
              lineHeight: 1.6,
              fontFamily: '"JetBrains Mono", ui-monospace, monospace',
              scrollBeyondLastLine: false,
            }}
          />
        ) : (
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
              readOnly: !editable,
              minimap: { enabled: false },
              fontSize: 13,
              lineHeight: 1.6,
              fontFamily: '"JetBrains Mono", ui-monospace, monospace',
              fontLigatures: true,
              scrollBeyondLastLine: false,
              tabSize: 2,
              renderWhitespace: 'none',
              renderLineHighlight: editable ? 'all' : 'none',
              guides: { indentation: true, highlightActiveIndentation: true },
              folding: true,
              smoothScrolling: true,
              cursorBlinking: 'smooth',
              overviewRulerLanes: 0,
              hideCursorInOverviewRuler: true,
              overviewRulerBorder: false,
              scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false },
              padding: { top: 12, bottom: 12 },
              contextmenu: editable,
            }}
          />
        )}
      </div>

      {editable && (
        <div className="flex items-center gap-3 border-t px-4 py-3">
          {applied ? (
            <span className="inline-flex items-center gap-1.5 text-sm text-[color:var(--ok)]">
              <Check className="size-4" /> {t('Applied to cluster')}
            </span>
          ) : confirming ? (
            <>
              <span className="inline-flex items-center gap-1.5 text-sm text-[color:var(--warn)]">
                <AlertTriangle className="size-4" /> {t('Apply to the live cluster?')}
              </span>
              <button
                onClick={apply}
                disabled={applying}
                className="inline-flex items-center gap-1.5 rounded-lg bg-[color:var(--err)]/90 px-3 py-1.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {applying ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                {t('Confirm apply')}
              </button>
              <button onClick={() => setConfirming(false)} className="rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground">
                {t('Back to edit')}
              </button>
            </>
          ) : (
            <>
              <button
                onClick={preview}
                disabled={!dirty || previewing || !!yamlError}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                  dirty && !yamlError ? 'bg-primary text-primary-foreground hover:opacity-90' : 'cursor-not-allowed bg-muted text-muted-foreground',
                  previewing && 'opacity-50',
                )}
              >
                {previewing ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                {t('Preview')}
              </button>
              {dirty && (
                <button
                  onClick={() => setValue(original.current)}
                  className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
                >
                  <RotateCcw className="size-4" /> {t('Discard')}
                </button>
              )}
              {yamlError ? (
                <span className="flex items-center gap-1.5 truncate text-xs text-[color:var(--err)]">
                  <AlertTriangle className="size-3.5 shrink-0" />
                  {t('Line')} {yamlError.line}: {yamlError.message}
                </span>
              ) : (
                error && <span className="truncate text-xs text-[color:var(--err)]">{error}</span>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
