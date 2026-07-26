import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { colorForPodIndex, detectLevel, fmtLogTs, highlightText, splitTimestamp } from './logs'

describe('detectLevel', () => {
  it('detects level from a level=X field', () => {
    expect(detectLevel('level=error something broke')).toBe('error')
    expect(detectLevel('lvl=warn retrying')).toBe('warn')
  })

  it('falls back to a bare level token', () => {
    expect(detectLevel('INFO starting server')).toBe('info')
    expect(detectLevel('DEBUG cache miss')).toBe('debug')
  })

  it('treats fatal/panic as error', () => {
    expect(detectLevel('PANIC: nil pointer')).toBe('error')
  })

  it('returns unknown when no level marker is present', () => {
    expect(detectLevel('just a plain line')).toBe('unknown')
  })
})

describe('splitTimestamp', () => {
  it('splits off a leading RFC3339 timestamp', () => {
    const { ts, msg } = splitTimestamp('2026-01-02T03:04:05.123456789Z hello world')
    expect(msg).toBe('hello world')
    expect(ts).toBeTypeOf('number')
  })

  it('leaves the line untouched when there is no timestamp', () => {
    const { ts, msg } = splitTimestamp('hello world')
    expect(ts).toBeUndefined()
    expect(msg).toBe('hello world')
  })
})

describe('fmtLogTs', () => {
  it('formats milliseconds as HH:MM:SS.mmm', () => {
    const d = new Date(2026, 0, 1, 9, 5, 3, 42)
    expect(fmtLogTs(d.getTime())).toBe('09:05:03.042')
  })
})

describe('colorForPodIndex', () => {
  it('is stable for the same index and cycles for larger indices', () => {
    expect(colorForPodIndex(0)).toBe(colorForPodIndex(0))
    expect(colorForPodIndex(0)).toBe(colorForPodIndex(8))
  })
})

describe('highlightText', () => {
  it('returns the text unchanged when there is no search term', () => {
    expect(highlightText('hello world', '')).toBe('hello world')
  })

  it('wraps every match in a <mark>', () => {
    const { container } = render(<>{highlightText('foo bar foo', 'foo')}</>)
    expect(container.querySelectorAll('mark')).toHaveLength(2)
    expect(container.textContent).toBe('foo bar foo')
  })
})
