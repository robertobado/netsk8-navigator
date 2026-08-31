import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merge Tailwind classes with conditional logic (the shadcn/ui `cn` helper). */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Opens an external URL in the user's default browser. A plain
 * `<a target="_blank">` works in the plain browser build, but is silently
 * swallowed in the native desktop app: WKWebView (macOS) and WebView2
 * (Windows) only create a new window/tab for a target="_blank" navigation
 * if the host app implements a delegate for it, and Wails' desktop shell
 * doesn't — the click just does nothing, with no error.
 *
 * The desktop app's window also never gets Wails' injected window.wails/
 * window.runtime JS bridge in the first place (its window navigates away
 * from Wails' own asset server to this app's real HTTP origin right at
 * startup — see backend/cmd/desktop/main.go's bootstrapRedirect comment),
 * so BrowserOpenURL can't be called from JS directly. POST
 * /api/open-external instead: in the desktop build that's wired to the
 * native runtime.BrowserOpenURL call (backend/internal/api/externalopen.go);
 * in the plain server/browser build it 501s and we fall back to the normal
 * window.open below, same as any other web page.
 */
export async function openExternal(url: string) {
  try {
    const res = await fetch('/api/open-external', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
    })
    if (res.ok) return
  } catch {
    // network/fetch failure (e.g. no backend at all) — fall through below
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

/** Turn an ISO creation timestamp into a compact k8s-style age (e.g. "3d", "2h"). */
export function age(iso?: string): string {
  if (!iso) return '—'
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '—'
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h`
  const days = Math.floor(hrs / 24)
  if (days < 365) return `${days}d`
  return `${Math.floor(days / 365)}y`
}

/** Shorten an EKS ARN context name to just the cluster name for display. */
export function shortContext(name: string): string {
  const m = /cluster\/(.+)$/.exec(name)
  return m ? m[1] : name
}
