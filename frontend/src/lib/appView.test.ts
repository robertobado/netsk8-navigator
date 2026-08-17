import { describe, expect, it } from 'vitest'
import { issueToPod, ov, viewTitles } from './appView'
import type { IssueItem } from '@/lib/api'

describe('viewTitles', () => {
  it('provides a title for every special (non-catalog) view', () => {
    const titles = viewTitles((key: string) => key)
    expect(titles.overview).toBeTruthy()
    expect(titles.pods).toBe('Pods')
    expect(titles.topology).toBeTruthy()
    expect(titles.events).toBeTruthy()
    expect(titles.helm).toBeTruthy()
  })
})

describe('ov', () => {
  it('renders an em dash for undefined', () => {
    expect(ov(undefined)).toBe('—')
  })
  it('locale-formats a defined number', () => {
    expect(ov(1234)).toBe((1234).toLocaleString('pt-BR'))
  })
  it('renders zero as "0", not the em-dash placeholder', () => {
    expect(ov(0)).toBe((0).toLocaleString('pt-BR'))
  })
})

describe('issueToPod', () => {
  it('maps a pod issue into the shape PodDrawer/PodsTable expect', () => {
    const issue: IssueItem = {
      kind: 'pod',
      namespace: 'prod',
      name: 'web-1',
      since: '2024-01-01T00:00:00Z',
      reason: 'CrashLoopBackOff',
      message: 'back-off restarting failed container',
      containers: ['app'],
    }
    const pod = issueToPod(issue)
    expect(pod.name).toBe('web-1')
    expect(pod.namespace).toBe('prod')
    expect(pod.status).toBe('CrashLoopBackOff')
    expect(pod.reason).toBe('CrashLoopBackOff')
    expect(pod.age).toBe('2024-01-01T00:00:00Z')
    expect(pod.containers).toEqual(['app'])
    expect(pod.ready).toBe(0)
    expect(pod.total).toBe(0)
    expect(pod.restarts).toBe(0)
  })

  it('falls back namespace to empty string and status to "Pending" when reason is empty', () => {
    const issue: IssueItem = { kind: 'node', name: 'node-1', since: '2024-01-01T00:00:00Z', reason: '', message: '' }
    const pod = issueToPod(issue)
    expect(pod.namespace).toBe('')
    expect(pod.status).toBe('Pending')
    expect(pod.containers).toEqual([])
  })
})
