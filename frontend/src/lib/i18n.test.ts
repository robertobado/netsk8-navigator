import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { LANGUAGES, setLanguage, tAgo, tf, useT } from './i18n'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))

// preferences.ts holds language in a module-level singleton that
// localStorage.clear() alone doesn't reset (it only affects what a future
// load() would read) — reset it explicitly so these pt-BR-assuming tests
// don't inherit 'en' left behind by another test file (e.g.
// preferences.test.ts sets language: 'en' and never restores it) when the
// suite runs without per-file module isolation.
beforeEach(() => {
  localStorage.clear()
  setLanguage('pt-BR')
})
afterEach(() => localStorage.clear())

describe('useT (pt-BR, the default language)', () => {
  it('translates a known key', () => {
    const { result } = renderHook(() => useT())
    expect(result.current('Delete')).toBe('Excluir')
  })

  it('falls back to the literal key when untranslated', () => {
    const { result } = renderHook(() => useT())
    expect(result.current('Some untranslated string')).toBe('Some untranslated string')
  })

  it('translates the static prefix of a "<word> <number>" key, keeping the number', () => {
    const { result } = renderHook(() => useT())
    expect(result.current('Revision 3')).toBe('Revisão 3')
  })

  it('translates the "from "/"to " connector prefix as a last resort', () => {
    const { result } = renderHook(() => useT())
    expect(result.current('from somewhere')).toBe('de somewhere')
    expect(result.current('to somewhere')).toBe('para somewhere')
  })
})

describe('useT (English)', () => {
  it('renders literal English keys unchanged and never falls back to pt', () => {
    setLanguage('en')
    const { result } = renderHook(() => useT())
    expect(result.current('Delete')).toBe('Delete')
    expect(result.current('from somewhere')).toBe('from somewhere') // no PT-only rewriting in en mode
  })
})

describe('tAgo', () => {
  it('formats "{d} ago" as a suffix', () => {
    const t = (_key: string, fallback?: string) => fallback ?? _key
    expect(tAgo(t, '5m')).toBe('5m ago')
  })
})

describe('tf', () => {
  it('substitutes {name}-style placeholders', () => {
    const t = () => 'View {kind} details'
    expect(tf(t, 'View {kind} details', { kind: 'Pod' })).toBe('View Pod details')
  })
})

describe('LANGUAGES', () => {
  it('lists pt-BR and en', () => {
    expect(LANGUAGES.map((l) => l.code)).toEqual(['pt-BR', 'en'])
  })
})
