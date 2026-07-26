import { describe, expect, it } from 'vitest'
import { crdView } from './nav'

describe('crdView', () => {
  it('builds a view key from a route CRD', () => {
    const key = crdView({
      group: 'gateway.networking.k8s.io',
      version: 'v1',
      resource: 'httproutes',
      kind: 'HTTPRoute',
      namespaced: true,
      label: 'HTTPRoutes',
      order: 2,
    })
    expect(key).toBe('crd:gateway.networking.k8s.io/v1/httproutes')
  })
})
