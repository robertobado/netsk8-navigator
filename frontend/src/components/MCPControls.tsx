import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Bot, Check, Copy, Eye, EyeOff, Plus, RefreshCw, X } from 'lucide-react'
import { api, type ContextInfo, regenerateMCPToken } from '@/lib/api'
import { useMcpGate, setMcpGate, type McpGate } from '@/lib/mcpGate'
import { useT } from '@/lib/i18n'
import { Switch } from '@/components/Switch'

export function MCPControls() {
  const t = useT()
  const mcp = useMcpGate()
  const queryClient = useQueryClient()
  const [confirmingWrite, setConfirmingWrite] = useState(false)
  const [confirmingRegenerate, setConfirmingRegenerate] = useState(false)
  const [tokenRevealed, setTokenRevealed] = useState(false)
  const [copied, setCopied] = useState<'url' | 'token' | null>(null)
  const [gateError, setGateError] = useState<string | null>(null)
  const mcpUrl = `${window.location.origin}/mcp`

  // Every gate change is one awaited PUT /api/mcp/gate. On failure the local
  // state is left untouched (the backend never accepted the change) and the
  // error is shown, so the switches keep telling the truth.
  const applyGate = (patch: Partial<McpGate>) => {
    setGateError(null)
    setMcpGate(patch).catch((e) => setGateError(e instanceof Error ? e.message : String(e)))
  }

  const tokenQ = useQuery({ queryKey: ['mcp-token'], queryFn: api.mcpToken, enabled: mcp.enabled, refetchInterval: false })
  const contextsQ = useQuery({ queryKey: ['contexts'], queryFn: api.contexts, enabled: mcp.enabled && mcp.allowWrite, refetchInterval: false })
  // Shares App.tsx's own ['health'] query — same key, same cache, no extra request.
  const healthQ = useQuery({ queryKey: ['health'], queryFn: api.health, staleTime: Infinity, refetchInterval: false })

  const toggleEnabled = () => {
    // Turning MCP off also force-clears allowWrite server-side (the backend
    // ANDs the two gates), so re-enabling later never silently re-arms
    // writes. readOnlyContexts is a hardening list, not a grant, so it
    // survives.
    applyGate({ enabled: !mcp.enabled })
    setConfirmingWrite(false)
  }

  const disableWrite = () => applyGate({ allowWrite: false })
  const confirmWrite = () => {
    applyGate({ allowWrite: true })
    setConfirmingWrite(false)
  }

  const addReadOnlyContext = (name: string) => {
    if (mcp.readOnlyContexts.includes(name)) return
    applyGate({ readOnlyContexts: [...mcp.readOnlyContexts, name] })
  }
  const removeReadOnlyContext = (name: string) => applyGate({ readOnlyContexts: mcp.readOnlyContexts.filter((c) => c !== name) })

  const regenerateToken = async () => {
    await regenerateMCPToken()
    setConfirmingRegenerate(false)
    await queryClient.invalidateQueries({ queryKey: ['mcp-token'] })
  }

  const copy = async (which: 'url' | 'token', text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(which)
      setTimeout(() => setCopied(null), 1500)
    } catch {
      // clipboard access denied — nothing useful to do
    }
  }

  const token = tokenQ.data?.token ?? ''
  const maskedToken = token ? `${token.slice(0, 6)}${'•'.repeat(Math.max(token.length - 6, 6))}` : ''

  let allowWriteToggle: ReactNode
  if (mcp.allowWrite) {
    allowWriteToggle = <Switch checked={true} onChange={disableWrite} label={t('controls.mcpAllowWrite')} activeClassName="bg-[color:var(--err)]" />
  } else if (confirmingWrite) {
    allowWriteToggle = (
      <div className="flex flex-col items-end gap-1.5">
        {healthQ.data && !healthQ.data.authEnabled && (
          <p className="max-w-full text-right text-[11px] text-[color:var(--err)]">{t('controls.mcpAllowWriteNoAuthWarning')}</p>
        )}
        <div className="flex items-center gap-1.5 rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-2 py-1">
          <AlertTriangle className="size-3.5 shrink-0 text-[color:var(--err)]" />
          <button type="button" onClick={confirmWrite} className="rounded-md bg-[color:var(--err)]/90 px-2 py-0.5 text-[11px] font-medium text-white">
            {t('Confirm')}
          </button>
          <button type="button" onClick={() => setConfirmingWrite(false)} className="text-[11px] text-muted-foreground hover:text-foreground">
            {t('Cancel')}
          </button>
        </div>
      </div>
    )
  } else {
    allowWriteToggle = <Switch checked={false} onChange={() => setConfirmingWrite(true)} label={t('controls.mcpAllowWrite')} />
  }

  return (
    <div className="space-y-3 rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          <Bot className="size-3.5 text-[color:var(--brand)]" /> {t('controls.mcp')}
        </span>
        <Switch checked={mcp.enabled} onChange={toggleEnabled} label={t('controls.mcp')} />
      </div>

      {gateError && (
        <p className="rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-2.5 py-1.5 text-[11px] text-[color:var(--err)]">
          {t('controls.mcpGateError', 'Could not update the MCP gate — try again.')} ({gateError})
        </p>
      )}

      {mcp.enabled && (
        <>
          <button
            type="button"
            onClick={() => copy('url', mcpUrl)}
            className="inline-flex w-full items-center justify-between gap-1.5 rounded-lg border bg-background/50 px-2.5 py-1.5 font-mono text-xs transition-colors hover:bg-accent"
            title={t('Copy')}
          >
            <span className="min-w-0 flex-1 truncate text-left">{mcpUrl}</span>
            {copied === 'url' ? <Check className="size-3.5 shrink-0 text-[color:var(--ok)]" /> : <Copy className="size-3.5 shrink-0 text-muted-foreground" />}
          </button>

          {token && (
            <div className="space-y-1">
              <span className="text-xs text-muted-foreground">{t('controls.mcpToken')}</span>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => copy('token', token)}
                  className="inline-flex min-w-0 flex-1 items-center justify-between gap-1.5 rounded-lg border bg-background/50 px-2.5 py-1.5 font-mono text-xs transition-colors hover:bg-accent"
                  title={t('Copy')}
                >
                  <span className="min-w-0 flex-1 truncate text-left">{tokenRevealed ? token : maskedToken}</span>
                  {copied === 'token' ? (
                    <Check className="size-3.5 shrink-0 text-[color:var(--ok)]" />
                  ) : (
                    <Copy className="size-3.5 shrink-0 text-muted-foreground" />
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => setTokenRevealed((v) => !v)}
                  className="shrink-0 rounded-lg border bg-background/50 p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  title={tokenRevealed ? t('Hide') : t('Reveal')}
                >
                  {tokenRevealed ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                </button>
                {confirmingRegenerate ? (
                  <div className="flex shrink-0 items-center gap-1.5 rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-2 py-1">
                    <button
                      type="button"
                      onClick={() => void regenerateToken()}
                      className="rounded-md bg-[color:var(--err)]/90 px-2 py-0.5 text-[11px] font-medium text-white"
                    >
                      {t('Confirm')}
                    </button>
                    <button type="button" onClick={() => setConfirmingRegenerate(false)} className="text-[11px] text-muted-foreground hover:text-foreground">
                      {t('Cancel')}
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirmingRegenerate(true)}
                    className="shrink-0 rounded-lg border bg-background/50 p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    title={t('controls.mcpRegenerateToken')}
                  >
                    <RefreshCw className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          )}

          <p className="text-[11px] text-muted-foreground">{t('controls.mcpInstallHint')}</p>

          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted-foreground">{t('controls.mcpAllowWrite')}</span>

            {allowWriteToggle}
          </div>

          {mcp.allowWrite && (
            <ReadOnlyContextsPicker
              contexts={contextsQ.data ?? []}
              readOnlyContexts={mcp.readOnlyContexts}
              onAdd={addReadOnlyContext}
              onRemove={removeReadOnlyContext}
            />
          )}
        </>
      )}
    </div>
  )
}

// ReadOnlyContextsPicker pins specific contexts (e.g. a production cluster)
// permanently read-only, independent of the global "Allow write" toggle.
// Deliberately an add/remove picker rather than one toggle per context: a
// real kubeconfig can have dozens of contexts, and a wall of individually
// toggled switches (almost all off) doesn't scale and reads as a confusing
// double negative. This only ever renders what's actually pinned.
function ReadOnlyContextsPicker({
  contexts,
  readOnlyContexts,
  onAdd,
  onRemove,
}: Readonly<{
  contexts: ContextInfo[]
  readOnlyContexts: string[]
  onAdd: (name: string) => void
  onRemove: (name: string) => void
}>) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const triggerRef = useRef<HTMLDivElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number; width: number } | null>(null)

  // The dropdown is portaled to document.body (see the render below) instead
  // of living in normal flow: every preferences card has backdrop-blur-xl,
  // which establishes its own CSS stacking context, so a merely-high z-index
  // on a dropdown nested inside one card can never paint above a sibling
  // card's own stacking context (e.g. Theme/Language, which come later in
  // the DOM) — it always renders "underneath" them regardless of z-index.
  // Portaling escapes that entirely; position is tracked via getBoundingClientRect
  // (same approach as HoverBubble.tsx) since a portaled node can't rely on
  // its original ancestor's layout for `position: absolute`.
  useEffect(() => {
    if (!open) return
    const updatePos = () => {
      const r = triggerRef.current?.getBoundingClientRect()
      if (r) setPos({ top: r.bottom + 4, left: r.left, width: r.width })
    }
    updatePos()
    function onClick(e: MouseEvent) {
      const target = e.target as Node
      if (triggerRef.current?.contains(target) || popoverRef.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    window.addEventListener('resize', updatePos)
    // capture:true so this also fires for scroll on PreferencesDialog's own
    // scrollable body, not just window-level scroll.
    window.addEventListener('scroll', updatePos, true)
    return () => {
      document.removeEventListener('mousedown', onClick)
      window.removeEventListener('resize', updatePos)
      window.removeEventListener('scroll', updatePos, true)
    }
  }, [open])

  const available = contexts.filter((c) => !readOnlyContexts.includes(c.name) && c.name.toLowerCase().includes(query.toLowerCase()))

  return (
    <div className="space-y-1.5 border-t pt-2">
      <span className="text-xs text-muted-foreground">{t('controls.mcpReadOnlyContexts')}</span>

      {readOnlyContexts.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {readOnlyContexts.map((name) => (
            <span key={name} className="inline-flex max-w-full items-center gap-1 rounded-full border bg-background/50 py-0.5 pr-1 pl-2 text-[11px]">
              <span className="max-w-40 truncate" title={name}>
                {name}
              </span>
              <button
                type="button"
                onClick={() => onRemove(name)}
                aria-label={`${t('Remove')} ${name}`}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}

      <div ref={triggerRef}>
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="flex w-full items-center gap-1.5 rounded-lg border border-dashed bg-background/30 px-2.5 py-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent"
        >
          <Plus className="size-3.5" /> {t('controls.mcpAddReadOnlyContext')}
        </button>
      </div>

      {open &&
        pos &&
        createPortal(
          <div
            ref={popoverRef}
            style={{ position: 'fixed', top: pos.top, left: pos.left, width: pos.width }}
            className="z-[95] overflow-hidden rounded-lg border bg-popover/95 shadow-2xl shadow-black/40 backdrop-blur-2xl"
          >
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('ns.search')}
              className="w-full border-b bg-transparent px-2.5 py-1.5 text-xs outline-none placeholder:text-muted-foreground"
            />
            <div className="max-h-40 overflow-y-auto p-1">
              {available.length === 0 && <p className="px-2 py-1.5 text-[11px] text-muted-foreground">{t('controls.mcpNoMoreContexts')}</p>}
              {available.map((c) => (
                <button
                  key={c.name}
                  type="button"
                  onClick={() => {
                    onAdd(c.name)
                    setQuery('')
                  }}
                  className="block w-full truncate rounded-md px-2 py-1 text-left text-[11px] hover:bg-accent"
                  title={c.name}
                >
                  {c.name}
                </button>
              ))}
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
