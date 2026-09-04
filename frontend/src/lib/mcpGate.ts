import { useSyncExternalStore } from 'react'

// The /mcp security gate — deliberately its own store, NOT a field of the
// AppPreferences blob in preferences.ts.
//
// It used to ride that blob, and the blob is mirrored to the backend with a
// best-effort, unordered, error-swallowed `fetch().catch(() => {})`. Two
// quick toggles (enable "Allow write", then pin a context read-only) could
// land at the backend out of order and leave its gate desynced from this
// UI — an "Allow write" that reads ON here but is OFF server-side, so an
// MCP `delete_resource` gets refused with "enable 'Allow write'". Since
// v0.0.27 gave the desktop app a stable origin, localStorage now persists
// across launches, so that desync stopped self-healing on restart.
//
// This store fixes that: every mutation is one AWAITED `PUT /api/mcp/gate`,
// the backend is the single source of truth, and the response — the
// canonical post-merge state — is what we adopt. A failed write throws so
// the caller can surface it; the UI never claims a state the backend didn't
// accept. localStorage is only a no-flash cache and the server always wins.
export interface McpGate {
  enabled: boolean
  allowWrite: boolean
  readOnlyContexts: string[]
  readDisabledContexts: string[]
}

const DEFAULTS: McpGate = { enabled: false, allowWrite: false, readOnlyContexts: [], readDisabledContexts: [] }
const LS_KEY = 'netsk8.mcpgate'
const ENDPOINT = '/api/mcp/gate'

function sanitizeStringArray(raw: unknown, fallback: string[]): string[] {
  return Array.isArray(raw) ? raw.filter((c): c is string => typeof c === 'string') : fallback
}

// Applied to both the localStorage read and every server response — neither
// is guaranteed to match this shape (an older client, a tampered value).
// The enabled/allowWrite invariant is mirrored from the backend so a stale
// allowWrite:true can never render as granted while MCP is off.
function sanitize(raw: unknown): McpGate {
  if (typeof raw !== 'object' || raw === null) return { ...DEFAULTS }
  const m = raw as Record<string, unknown>
  const enabled = typeof m.enabled === 'boolean' ? m.enabled : DEFAULTS.enabled
  return {
    enabled,
    allowWrite: enabled && typeof m.allowWrite === 'boolean' ? m.allowWrite : false,
    readOnlyContexts: sanitizeStringArray(m.readOnlyContexts, DEFAULTS.readOnlyContexts),
    readDisabledContexts: sanitizeStringArray(m.readDisabledContexts, DEFAULTS.readDisabledContexts),
  }
}

function load(): McpGate {
  try {
    return sanitize(JSON.parse(localStorage.getItem(LS_KEY) ?? '{}'))
  } catch {
    return { ...DEFAULTS }
  }
}

let state: McpGate = load()
const listeners = new Set<() => void>()

function commit(next: McpGate) {
  state = next
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(state))
  } catch {
    // private-mode / quota — the cache is optional, the server has the truth
  }
  for (const l of listeners) l()
}

/**
 * Applies a partial change to the gate through the one authoritative,
 * ordered endpoint and adopts the backend's canonical response. Rejects
 * (without mutating local state) when the write fails, so callers can show
 * the error instead of the UI drifting from the server.
 */
export async function setMcpGate(patch: Partial<McpGate>): Promise<void> {
  const res = await fetch(ENDPOINT, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!res.ok) throw new Error(`gate update failed (${res.status})`)
  commit(sanitize(await res.json()))
}

// Pulls the authoritative gate from the backend once at startup. Server
// wins unconditionally: this is a security control, not a convenience
// preference, so a value this browser never managed to push up must not
// linger just because it's in localStorage.
let hydrated = false
export function hydrateMcpGate() {
  if (hydrated) return
  hydrated = true
  fetch(ENDPOINT)
    .then((r) => (r.ok ? r.json() : null))
    .then((remote) => {
      if (remote && typeof remote === 'object') commit(sanitize(remote))
    })
    .catch(() => {})
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useMcpGate(): McpGate {
  return useSyncExternalStore(subscribe, () => state)
}
