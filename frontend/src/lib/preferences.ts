import { useSyncExternalStore } from 'react'

// App-wide preferences: one typed object, localStorage-first (no load flash) and
// mirrored to the backend (`/api/preferences`) so they persist across browsers
// and survive as the app moves toward native builds. Per-widget ephemeral state
// (e.g. table sort order) stays in its own localStorage keys.
export type ThemeMode = 'light' | 'dark' | 'auto'
const THEME_MODES: readonly ThemeMode[] = ['light', 'dark', 'auto']

/** Ordered for display: the toggle renders one button per entry, in this order. */
export const THEME_MODE_OPTIONS: ReadonlyArray<{ mode: ThemeMode; labelKey: string }> = [
  { mode: 'light', labelKey: 'theme.light' },
  { mode: 'dark', labelKey: 'theme.dark' },
  { mode: 'auto', labelKey: 'theme.auto' },
]

export interface AppPreferences {
  language: string // 'pt-BR' (future: 'en', …)
  metricsRefreshMs: number // 0 = off
  background: { enabled: boolean; effect: string; opacity: number }
  theme: ThemeMode // 'auto' follows the OS/browser color-scheme preference
  // NB: the /mcp security gate (enabled / allowWrite / per-context pins)
  // used to live here. It moved to its own store + endpoint — see
  // lib/mcpGate.ts — because this blob's fire-and-forget mirror could
  // desync it from the backend.
  // Contexts starred in the kubeconfig manager / context switcher — purely
  // a client-side convenience, never sent to the backend beyond this blob.
  contexts: { favorites: string[] }
}

const DEFAULTS: AppPreferences = {
  language: 'pt-BR',
  metricsRefreshMs: 15_000,
  background: { enabled: false, effect: 'net', opacity: 0.6 },
  theme: 'dark', // the app predates light mode — keep existing users on dark by default
  contexts: { favorites: [] },
}

// Reflects the theme choice onto <html data-theme> so index.css can key off
// it: 'light'/'dark' force that palette; 'auto' clears the attribute so the
// `@media (prefers-color-scheme)` rules (which only apply with no override)
// take over and track OS changes live with no JS listener needed.
function applyTheme(theme: ThemeMode) {
  if (typeof document === 'undefined') return
  if (theme === 'auto') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = theme
}

const LS_KEY = 'netsk8.prefs'

function sanitizeBackground(raw: unknown): AppPreferences['background'] | undefined {
  if (typeof raw !== 'object' || raw === null) return undefined
  const b = raw as Record<string, unknown>
  return {
    enabled: typeof b.enabled === 'boolean' ? b.enabled : DEFAULTS.background.enabled,
    effect: typeof b.effect === 'string' ? b.effect : DEFAULTS.background.effect,
    opacity: typeof b.opacity === 'number' ? b.opacity : DEFAULTS.background.opacity,
  }
}

function sanitizeStringArray(raw: unknown, fallback: string[]): string[] {
  return Array.isArray(raw) ? raw.filter((c): c is string => typeof c === 'string') : fallback
}

function sanitizeContexts(raw: unknown): AppPreferences['contexts'] | undefined {
  if (typeof raw !== 'object' || raw === null) return undefined
  const c = raw as Record<string, unknown>
  return { favorites: sanitizeStringArray(c.favorites, DEFAULTS.contexts.favorites) }
}

// Picks only the known, correctly-typed fields out of an arbitrary value before
// it's trusted as preferences — applied to both the localStorage read and the
// server's response, since neither is guaranteed to still match this shape
// (an older client version, a tampered value, a compromised/MITM'd response).
function sanitizePrefs(raw: unknown): Partial<AppPreferences> {
  if (typeof raw !== 'object' || raw === null) return {}
  const r = raw as Record<string, unknown>
  const out: Partial<AppPreferences> = {}
  if (typeof r.language === 'string') out.language = r.language
  if (typeof r.metricsRefreshMs === 'number') out.metricsRefreshMs = r.metricsRefreshMs
  if (typeof r.theme === 'string' && (THEME_MODES as string[]).includes(r.theme)) out.theme = r.theme as ThemeMode
  const background = sanitizeBackground(r.background)
  if (background) out.background = background
  const contexts = sanitizeContexts(r.contexts)
  if (contexts) out.contexts = contexts
  return out
}

function mergeDefaults(safe: Partial<AppPreferences>): AppPreferences {
  return {
    ...DEFAULTS,
    ...safe,
    background: { ...DEFAULTS.background, ...safe.background },
    contexts: { ...DEFAULTS.contexts, ...safe.contexts },
  }
}

function load(): AppPreferences {
  try {
    const raw = JSON.parse(localStorage.getItem(LS_KEY) ?? '{}')
    return mergeDefaults(sanitizePrefs(raw))
  } catch {
    return DEFAULTS
  }
}

let state: AppPreferences = load()
applyTheme(state.theme)
const listeners = new Set<() => void>()

function emit() {
  applyTheme(state.theme)
  for (const l of listeners) l()
}
function persist() {
  localStorage.setItem(LS_KEY, JSON.stringify(state))
  void fetch('/api/preferences', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(state),
  }).catch(() => {}) // best-effort mirror; localStorage is the source of truth
}

export function setAppPrefs(patch: Partial<AppPreferences>) {
  state = { ...state, ...patch }
  persist()
  emit()
}

// hydrateAppPrefs pulls server-side prefs once at startup (they win if present;
// otherwise the local defaults are pushed up).
let hydrated = false
export function hydrateAppPrefs() {
  if (hydrated) return
  hydrated = true
  fetch('/api/preferences')
    .then((r) => (r.ok ? r.json() : null))
    .then((remote) => {
      if (remote && typeof remote === 'object' && Object.keys(remote).length > 0) {
        state = mergeDefaults(sanitizePrefs(remote))
        localStorage.setItem(LS_KEY, JSON.stringify(state))
        emit()
      } else {
        persist()
      }
    })
    .catch(() => {})
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useAppPrefs(): AppPreferences {
  return useSyncExternalStore(subscribe, () => state)
}
