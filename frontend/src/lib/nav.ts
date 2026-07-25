import type { RouteKind } from '@/lib/api'

// Navigation primitives shared between App and ResourceNav. Kept out of the
// ResourceNav component file so React Fast Refresh isn't broken by non-component
// exports living next to the component.

export type View = 'overview' | 'pods' | 'topology' | 'events'

/** View key for a route CRD list (e.g. "crd:gateway.networking.k8s.io/v1/httproutes"). */
export function crdView(rk: RouteKind): string {
  return `crd:${rk.group}/${rk.version}/${rk.resource}`
}
