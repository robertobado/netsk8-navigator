import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Bot, Check, Copy, Eye, EyeOff, RefreshCw } from 'lucide-react'
import { api, regenerateMCPToken } from '@/lib/api'
import { useAppPrefs, setAppPrefs } from '@/lib/preferences'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

export function MCPControls() {
  const t = useT()
  const { mcp } = useAppPrefs()
  const queryClient = useQueryClient()
  const [confirmingWrite, setConfirmingWrite] = useState(false)
  const [confirmingRegenerate, setConfirmingRegenerate] = useState(false)
  const [tokenRevealed, setTokenRevealed] = useState(false)
  const [copied, setCopied] = useState<'url' | 'token' | null>(null)
  const mcpUrl = `${window.location.origin}/mcp`

  const tokenQ = useQuery({ queryKey: ['mcp-token'], queryFn: api.mcpToken, enabled: mcp.enabled, refetchInterval: false })
  const contextsQ = useQuery({ queryKey: ['contexts'], queryFn: api.contexts, enabled: mcp.enabled && mcp.allowWrite, refetchInterval: false })

  const toggleEnabled = () => {
    // Turning MCP off also force-clears allowWrite, so re-enabling later
    // never silently re-arms writes — mirrors the backend's own AND gate.
    // readOnlyContexts is a hardening list, not a grant, so it survives.
    setAppPrefs({ mcp: { ...mcp, enabled: !mcp.enabled, allowWrite: mcp.enabled ? false : mcp.allowWrite } })
    setConfirmingWrite(false)
  }

  const disableWrite = () => setAppPrefs({ mcp: { ...mcp, allowWrite: false } })
  const confirmWrite = () => {
    setAppPrefs({ mcp: { ...mcp, allowWrite: true } })
    setConfirmingWrite(false)
  }

  const toggleReadOnlyContext = (name: string) => {
    const readOnlyContexts = mcp.readOnlyContexts.includes(name) ? mcp.readOnlyContexts.filter((c) => c !== name) : [...mcp.readOnlyContexts, name]
    setAppPrefs({ mcp: { ...mcp, readOnlyContexts } })
  }

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

  return (
    <div className="space-y-3 rounded-xl border bg-background/40 p-3 backdrop-blur-xl">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          <Bot className="size-3.5 text-[color:var(--brand)]" /> {t('controls.mcp')}
        </span>
        <button
          type="button"
          role="switch"
          aria-checked={mcp.enabled}
          aria-label={t('controls.mcp')}
          onClick={toggleEnabled}
          className={cn('relative h-5 w-9 shrink-0 rounded-full transition-colors', mcp.enabled ? 'bg-[color:var(--brand)]' : 'bg-muted')}
        >
          <span className={cn('absolute top-0.5 size-4 rounded-full bg-white shadow transition-all', mcp.enabled ? 'left-[1.125rem]' : 'left-0.5')} />
        </button>
      </div>

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

            {mcp.allowWrite ? (
              <button
                type="button"
                role="switch"
                aria-checked={true}
                aria-label={t('controls.mcpAllowWrite')}
                onClick={disableWrite}
                className="relative h-5 w-9 shrink-0 rounded-full bg-[color:var(--err)] transition-colors"
              >
                <span className="absolute top-0.5 left-[1.125rem] size-4 rounded-full bg-white shadow" />
              </button>
            ) : confirmingWrite ? (
              <div className="flex items-center gap-1.5 rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-2 py-1">
                <AlertTriangle className="size-3.5 shrink-0 text-[color:var(--err)]" />
                <button type="button" onClick={confirmWrite} className="rounded-md bg-[color:var(--err)]/90 px-2 py-0.5 text-[11px] font-medium text-white">
                  {t('Confirm')}
                </button>
                <button type="button" onClick={() => setConfirmingWrite(false)} className="text-[11px] text-muted-foreground hover:text-foreground">
                  {t('Cancel')}
                </button>
              </div>
            ) : (
              <button
                type="button"
                role="switch"
                aria-checked={false}
                aria-label={t('controls.mcpAllowWrite')}
                onClick={() => setConfirmingWrite(true)}
                className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition-colors"
              >
                <span className="absolute top-0.5 left-0.5 size-4 rounded-full bg-white shadow" />
              </button>
            )}
          </div>

          {mcp.allowWrite && contextsQ.data && contextsQ.data.length > 0 && (
            <div className="space-y-1 border-t pt-2">
              <span className="text-xs text-muted-foreground">{t('controls.mcpReadOnlyContexts')}</span>
              <div className="max-h-32 space-y-1 overflow-y-auto">
                {contextsQ.data.map((c) => {
                  const readOnly = mcp.readOnlyContexts.includes(c.name)
                  return (
                    <div key={c.name} className="flex items-center justify-between gap-2">
                      <span className="min-w-0 flex-1 truncate text-[11px]" title={c.name}>
                        {c.name}
                      </span>
                      <button
                        type="button"
                        role="switch"
                        aria-checked={readOnly}
                        aria-label={`${t('controls.mcpReadOnlyContexts')}: ${c.name}`}
                        onClick={() => toggleReadOnlyContext(c.name)}
                        className={cn('relative h-4 w-7 shrink-0 rounded-full transition-colors', readOnly ? 'bg-[color:var(--brand)]' : 'bg-muted')}
                      >
                        <span
                          className={cn('absolute top-0.5 size-3 rounded-full bg-white shadow transition-all', readOnly ? 'left-[0.875rem]' : 'left-0.5')}
                        />
                      </button>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
