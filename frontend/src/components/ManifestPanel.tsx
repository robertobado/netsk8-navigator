import { useEffect, useRef, useState } from 'react'
import Editor from '@monaco-editor/react'
import { AlertTriangle, Check, Copy, Loader2, Lock, RotateCcw, Save } from 'lucide-react'
import '@/lib/monaco'
import { ensureNetsk8Theme, NETSK8_THEME } from '@/lib/monacoTheme'
import { applyManifest, getManifest, type ManifestKind } from '@/lib/api'
import { cn } from '@/lib/utils'

type State = 'loading' | 'ready' | 'error'

// Monaco-based YAML viewer/editor for a resource manifest. Editing mutates the
// live cluster, so applying requires an explicit two-step confirm.
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
  const [state, setState] = useState<State>('loading')
  const [error, setError] = useState('')
  const [value, setValue] = useState('')
  const original = useRef('')
  const [confirming, setConfirming] = useState(false)
  const [applying, setApplying] = useState(false)
  const [applied, setApplied] = useState(false)
  const [copied, setCopied] = useState(false)

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
        <Loader2 className="mr-2 size-4 animate-spin" /> Carregando manifest...
      </div>
    )
  if (state === 'error')
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-sm text-[color:var(--err)]">
        {error}
      </div>
    )

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-1.5">
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          {!editable && <Lock className="size-3" />}
          {editable ? 'YAML' : 'YAML · somente leitura'}
        </span>
        <button
          onClick={copy}
          className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          title="Copiar YAML"
        >
          {copied ? <Check className="size-3.5 text-[color:var(--ok)]" /> : <Copy className="size-3.5" />}
          {copied ? 'Copiado' : 'Copiar'}
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        <Editor
          height="100%"
          language="yaml"
          theme={NETSK8_THEME}
          beforeMount={ensureNetsk8Theme}
          value={value}
          onChange={(v) => setValue(v ?? '')}
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
      </div>

      {editable && (
        <div className="flex items-center gap-3 border-t px-4 py-3">
          {applied ? (
            <span className="inline-flex items-center gap-1.5 text-sm text-[color:var(--ok)]">
              <Check className="size-4" /> Aplicado ao cluster
            </span>
          ) : confirming ? (
            <>
              <span className="inline-flex items-center gap-1.5 text-sm text-[color:var(--warn)]">
                <AlertTriangle className="size-4" /> Aplicar no cluster ao vivo?
              </span>
              <button
                onClick={apply}
                disabled={applying}
                className="inline-flex items-center gap-1.5 rounded-lg bg-[color:var(--err)]/90 px-3 py-1.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {applying ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                Confirmar
              </button>
              <button onClick={() => setConfirming(false)} className="rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground">
                Cancelar
              </button>
            </>
          ) : (
            <>
              <button
                onClick={() => setConfirming(true)}
                disabled={!dirty}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                  dirty ? 'bg-primary text-primary-foreground hover:opacity-90' : 'cursor-not-allowed bg-muted text-muted-foreground',
                )}
              >
                <Save className="size-4" /> Aplicar
              </button>
              {dirty && (
                <button
                  onClick={() => setValue(original.current)}
                  className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
                >
                  <RotateCcw className="size-4" /> Descartar
                </button>
              )}
              {error && <span className="truncate text-xs text-[color:var(--err)]">{error}</span>}
            </>
          )}
        </div>
      )}
    </div>
  )
}
