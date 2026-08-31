import { afterEach, describe, expect, it, vi } from 'vitest'
import { age, cn, openExternal, shortContext } from './utils'

describe('cn', () => {
  it('merges class names and resolves Tailwind conflicts', () => {
    expect(cn('px-2', 'py-1')).toBe('px-2 py-1')
    expect(cn('px-2', 'px-4')).toBe('px-4')
  })
  it('drops falsy values', () => {
    expect(cn('a', false, undefined, null, 'b')).toBe('a b')
  })
})

describe('age', () => {
  it('returns — for missing input', () => {
    expect(age(undefined)).toBe('—')
    expect(age('')).toBe('—')
  })
  it('returns — for an unparseable date', () => {
    expect(age('not-a-date')).toBe('—')
  })
  it('formats seconds', () => {
    expect(age(new Date(Date.now() - 5_000).toISOString())).toBe('5s')
  })
  it('formats minutes', () => {
    expect(age(new Date(Date.now() - 5 * 60_000).toISOString())).toBe('5m')
  })
  it('formats hours', () => {
    expect(age(new Date(Date.now() - 5 * 3_600_000).toISOString())).toBe('5h')
  })
  it('formats days', () => {
    expect(age(new Date(Date.now() - 5 * 86_400_000).toISOString())).toBe('5d')
  })
  it('formats years', () => {
    expect(age(new Date(Date.now() - 2 * 365 * 86_400_000).toISOString())).toBe('2y')
  })
})

describe('shortContext', () => {
  it('extracts the cluster name from an EKS ARN', () => {
    expect(shortContext('arn:aws:eks:us-east-1:123456789012:cluster/my-cluster')).toBe('my-cluster')
  })
  it('returns the name unchanged when there is no cluster/ segment', () => {
    expect(shortContext('minikube')).toBe('minikube')
  })
})

describe('openExternal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('POSTs to /api/open-external and does not fall back to window.open when that succeeds (desktop build)', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    const windowOpen = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('open', windowOpen)

    await openExternal('https://example.com')

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/open-external',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ url: 'https://example.com' }) }),
    )
    expect(windowOpen).not.toHaveBeenCalled()
  })

  it('falls back to window.open when /api/open-external 501s (plain browser build)', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 501 })
    const windowOpen = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('open', windowOpen)

    await openExternal('https://example.com')

    expect(windowOpen).toHaveBeenCalledWith('https://example.com', '_blank', 'noopener,noreferrer')
  })

  it('falls back to window.open when the fetch itself throws (no backend reachable)', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('network error'))
    const windowOpen = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('open', windowOpen)

    await openExternal('https://example.com')

    expect(windowOpen).toHaveBeenCalledWith('https://example.com', '_blank', 'noopener,noreferrer')
  })
})
