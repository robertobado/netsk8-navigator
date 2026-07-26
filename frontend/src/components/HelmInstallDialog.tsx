import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Editor, { type Monaco } from '@monaco-editor/react'
import type { editor as MonacoEditor } from 'monaco-editor'
import { AlertTriangle, ChevronLeft, Loader2, PackagePlus, Search, X } from 'lucide-react'
import '@/lib/monaco'
import { ensureNetsk8Theme, NETSK8_THEME } from '@/lib/monacoTheme'
import { helmChartDetail, helmSearch, installHelmRelease, upgradeHelmRelease, type HelmChartSummary, type HelmRelease } from '@/lib/api'
import { checkYamlSyntax } from '@/lib/yaml'
import { useYamlMarkers } from '@/lib/useYamlMarkers'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

interface Props {
  ctx: string
  mode: 'install' | 'upgrade'
  open: boolean
  namespace?: string // default/target namespace for a new install
  existingRelease?: { namespace: string; name: string } // required for upgrade
  initialValues?: string // upgrade: prefill with the release's current values
  onClose: () => void
  onDone: (release: HelmRelease) => void
}

// One dialog for both flows: search/pick a chart (repo + name + version), edit
// its values, then install a brand-new release or upgrade an existing one to
// that chart/version. Install additionally asks for a release name + namespace;
// upgrade reuses the existing release's identity.
export function HelmInstallDialog({ ctx, mode, open, namespace, existingRelease, initialValues, onClose, onDone }: Readonly<Props>) {
  const t = useT()
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<HelmChartSummary | null>(null)
  const [version, setVersion] = useState('')
  const [releaseName, setReleaseName] = useState('')
  const [ns, setNs] = useState(namespace || 'default')
  const [values, setValues] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const editorRef = useRef<MonacoEditor.IStandaloneCodeEditor | null>(null)
  const monacoRef = useRef<Monaco | null>(null)

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(null)
    setVersion('')
    setReleaseName('')
    setNs(namespace || 'default')
    setValues('')
    setError('')
  }, [open, namespace])

  const searchQ = useQuery({
    queryKey: ['helmSearch', query],
    queryFn: () => helmSearch(query),
    enabled: open && !selected,
  })
  const detailQ = useQuery({
    queryKey: ['helmChartDetail', selected?.repo, selected?.name],
    queryFn: () => helmChartDetail(selected!.repo, selected!.name),
    enabled: open && !!selected,
  })

  // Seed the editor once the chart's default values (install) or the existing
  // release's current values (upgrade) become available.
  useEffect(() => {
    if (!selected) return
    setValues(mode === 'upgrade' && initialValues ? initialValues : (detailQ.data?.defaultValues ?? ''))
    setVersion((v) => v || detailQ.data?.versions[0] || selected.version)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, detailQ.data])

  const yamlError = useMemo(() => checkYamlSyntax(values), [values])
  useYamlMarkers(editorRef, monacoRef, yamlError)

  if (!open) return null

  const canSubmit = !!selected && !yamlError && (mode === 'upgrade' || releaseName.trim() !== '')

  const submit = async () => {
    setBusy(true)
    setError('')
    try {
      const req = { repo: selected!.repo, chart: selected!.name, version, releaseName, namespace: ns, values }
      const rel =
        mode === 'install' ? await installHelmRelease(ctx, req) : await upgradeHelmRelease(ctx, existingRelease!.namespace, existingRelease!.name, req)
      onDone(rel)
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
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <PackagePlus className="size-4" /> {mode === 'install' ? t('Install chart') : t('Upgrade release')}
          </h2>
          <button onClick={onClose} className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
            <X className="size-4" />
          </button>
        </div>

        {!selected ? (
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex items-center gap-2 border-b px-4 py-2.5">
              <Search className="size-4 text-muted-foreground" />
              <input
                autoFocus
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t('Search charts...')}
                className="w-full bg-transparent py-1 text-sm outline-none placeholder:text-muted-foreground"
              />
            </div>
            <div className="min-h-0 flex-1 overflow-auto p-2">
              {searchQ.isLoading && (
                <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
                </div>
              )}
              {!searchQ.isLoading && (searchQ.data?.length ?? 0) === 0 && (
                <p className="py-8 text-center text-sm text-muted-foreground">{t('No charts found. Add a repository first.')}</p>
              )}
              {searchQ.data?.map((c) => (
                <button
                  key={`${c.repo}/${c.name}`}
                  onClick={() => setSelected(c)}
                  className="flex w-full flex-col gap-0.5 rounded-lg px-3 py-2 text-left transition-colors hover:bg-accent"
                >
                  <span className="flex items-center gap-2 text-sm font-medium">
                    {c.repo}/{c.name} <span className="font-mono text-xs text-muted-foreground">{c.version}</span>
                  </span>
                  {c.description && <span className="truncate text-xs text-muted-foreground">{c.description}</span>}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex flex-wrap items-center gap-3 border-b px-4 py-2.5">
              <button
                onClick={() => setSelected(null)}
                className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <ChevronLeft className="size-3.5" /> {t('Back')}
              </button>
              <span className="font-mono text-sm font-medium">
                {selected.repo}/{selected.name}
              </span>
              <select
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                disabled={!detailQ.data}
                className="rounded-md border bg-background/50 px-2 py-1 text-xs outline-none"
              >
                {(detailQ.data?.versions ?? [version]).map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
              {mode === 'install' && (
                <>
                  <input
                    value={releaseName}
                    onChange={(e) => setReleaseName(e.target.value)}
                    placeholder={t('Release name')}
                    className="w-36 rounded-md border bg-background/50 px-2 py-1 text-xs outline-none"
                  />
                  <input
                    value={ns}
                    onChange={(e) => setNs(e.target.value)}
                    placeholder={t('Namespace')}
                    className="w-28 rounded-md border bg-background/50 px-2 py-1 text-xs outline-none"
                  />
                </>
              )}
            </div>
            <div className="min-h-0 flex-1">
              {detailQ.isLoading ? (
                <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" /> {t('Loading...')}
                </div>
              ) : (
                <Editor
                  height="100%"
                  language="yaml"
                  theme={NETSK8_THEME}
                  beforeMount={ensureNetsk8Theme}
                  value={values}
                  onChange={(v) => setValues(v ?? '')}
                  onMount={(ed, monacoNs) => {
                    editorRef.current = ed
                    monacoRef.current = monacoNs
                  }}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 13,
                    lineHeight: 1.6,
                    fontFamily: '"JetBrains Mono", ui-monospace, monospace',
                    scrollBeyondLastLine: false,
                    tabSize: 2,
                  }}
                />
              )}
            </div>
          </div>
        )}

        <div className="flex items-center gap-3 border-t px-4 py-3">
          <button
            onClick={submit}
            disabled={!canSubmit || busy}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
              canSubmit ? 'bg-primary text-primary-foreground hover:opacity-90' : 'cursor-not-allowed bg-muted text-muted-foreground',
              busy && 'opacity-50',
            )}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <PackagePlus className="size-4" />}
            {mode === 'install' ? t('Install') : t('Upgrade')}
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
