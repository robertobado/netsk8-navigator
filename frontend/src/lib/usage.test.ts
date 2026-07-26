import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { fmtBytes, fmtCores, readBasis, usageSortValue, writeBasis } from './usage'

describe('fmtCores', () => {
  it('uses 3 decimals below 1 core', () => {
    expect(fmtCores(0.1234)).toBe('0.123')
  })
  it('uses 2 decimals at/above 1 core', () => {
    expect(fmtCores(1.2345)).toBe('1.23')
  })
})

describe('fmtBytes', () => {
  it('stays in bytes below 1024', () => {
    expect(fmtBytes(512)).toBe('512B')
  })
  it('scales up through the units', () => {
    expect(fmtBytes(1024)).toBe('1.0Ki')
    expect(fmtBytes(1024 * 1024)).toBe('1.0Mi')
    expect(fmtBytes(1024 * 1024 * 1024)).toBe('1.0Gi')
  })
  it('drops the decimal at 10 or above', () => {
    expect(fmtBytes(12 * 1024)).toBe('12Ki')
  })
})

describe('usageSortValue', () => {
  it('sinks missing gauges to -1', () => {
    expect(usageSortValue(undefined, 'pct')).toBe(-1)
  })
  it('returns absolute used for "abs" basis', () => {
    expect(usageSortValue({ used: 5, total: 10, unit: 'cores' }, 'abs')).toBe(5)
  })
  it('returns a ratio for "pct" basis', () => {
    expect(usageSortValue({ used: 5, total: 10, unit: 'cores' }, 'pct')).toBe(0.5)
  })
  it('sinks to -1 when there is no ceiling to compute a % against', () => {
    expect(usageSortValue({ used: 5, total: 0, unit: 'cores' }, 'pct')).toBe(-1)
  })
})

describe('readBasis/writeBasis', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => localStorage.clear())

  it('defaults to "pct" when nothing is stored', () => {
    expect(readBasis('cpu')).toBe('pct')
  })
  it('round-trips through localStorage', () => {
    writeBasis('cpu', 'abs')
    expect(readBasis('cpu')).toBe('abs')
  })
})
