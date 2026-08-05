import { useState } from 'react'
import { AlertTriangle, Bot, Check, Copy } from 'lucide-react'
import { useAppPrefs, setAppPrefs } from '@/lib/preferences'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

export function MCPControls() {
  const t = useT()
  const { mcp } = useAppPrefs()
  const [confirmingWrite, setConfirmingWrite] = useState(false)
  const [copied, setCopied] = useState(false)
  const mcpUrl = `${window.location.origin}/mcp`

  const toggleEnabled = () => {
    // Turning MCP off also force-clears allowWrite, so re-enabling later
    // never silently re-arms writes — mirrors the backend's own AND gate.
    setAppPrefs({ mcp: { enabled: !mcp.enabled, allowWrite: mcp.enabled ? false : mcp.allowWrite } })
    setConfirmingWrite(false)
  }

  const disableWrite = () => setAppPrefs({ mcp: { ...mcp, allowWrite: false } })
  const confirmWrite = () => {
    setAppPrefs({ mcp: { ...mcp, allowWrite: true } })
    setConfirmingWrite(false)
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(mcpUrl)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard access denied — nothing useful to do
    }
  }

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
            onClick={copy}
            className="inline-flex w-full items-center justify-between gap-1.5 rounded-lg border bg-background/50 px-2.5 py-1.5 font-mono text-xs transition-colors hover:bg-accent"
            title={t('Copy')}
          >
            <span className="min-w-0 flex-1 truncate text-left">{mcpUrl}</span>
            {copied ? <Check className="size-3.5 shrink-0 text-[color:var(--ok)]" /> : <Copy className="size-3.5 shrink-0 text-muted-foreground" />}
          </button>

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
        </>
      )}
    </div>
  )
}
