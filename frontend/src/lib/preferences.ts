import { useSyncExternalStore } from 'react'

// App-wide preferences: one typed object, localStorage-first (no load flash) and
// mirrored to the backend (`/api/preferences`) so they persist across browsers
// and survive as the app moves toward native builds. Per-widget ephemeral state
// (e.g. table sort order) stays in its own localStorage keys.
export interface AppPreferences {
  language: string // 'pt-BR' (future: 'en', …)
  metricsRefreshMs: number // 0 = off
  background: { enabled: boolean; effect: string; opacity: number }
}

const DEFAULTS: AppPreferences = {
  language: 'pt-BR',
  metricsRefreshMs: 15_000,
  background: { enabled: false, effect: 'net', opacity: 0.6 },
}

const LS_KEY = 'netsk8.prefs'

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
  if (typeof r.background === 'object' && r.background !== null) {
    const b = r.background as Record<string, unknown>
    out.background = {
      enabled: typeof b.enabled === 'boolean' ? b.enabled : DEFAULTS.background.enabled,
      effect: typeof b.effect === 'string' ? b.effect : DEFAULTS.background.effect,
      opacity: typeof b.opacity === 'number' ? b.opacity : DEFAULTS.background.opacity,
    }
  }
  return out
}

function load(): AppPreferences {
  try {
    const raw = JSON.parse(localStorage.getItem(LS_KEY) ?? '{}')
    const safe = sanitizePrefs(raw)
    return { ...DEFAULTS, ...safe, background: { ...DEFAULTS.background, ...safe.background } }
  } catch {
    return DEFAULTS
  }
}

let state: AppPreferences = load()
const listeners = new Set<() => void>()

function emit() {
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
        const safe = sanitizePrefs(remote)
        state = { ...DEFAULTS, ...safe, background: { ...DEFAULTS.background, ...safe.background } }
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
