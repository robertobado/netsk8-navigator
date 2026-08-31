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
 * doesn't — the click just does nothing, with no error. window.runtime's
 * BrowserOpenURL (the same call the native "Open in Browser" menu item
 * uses, see backend/cmd/desktop/main.go) is the one that actually works
 * there; it isn't present in the plain browser build, where the normal
 * window.open fallback below is what's needed instead.
 */
export function openExternal(url: string) {
  const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (url: string) => void } }).runtime
  if (rt?.BrowserOpenURL) {
    rt.BrowserOpenURL(url)
    return
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
