// Pure helpers pulled out of App.tsx: view titles, the issues→Pod adapter,
// and the overview stat-card number formatter. Kept in their own module
// (rather than exported from App.tsx) so Fast Refresh only sees components
// there, and so this logic is unit-testable without mounting the app tree.
import type { IssueItem, Pod } from '@/lib/api'
import type { View } from '@/lib/nav'
import type { TFunc } from '@/lib/i18n'

// Titles for the special (non-catalog) views; catalog resources and CRDs derive
// their own labels from the catalog / discovery.
export function viewTitles(t: TFunc): Record<View, string> {
  return {
    overview: t('Cluster overview'),
    pods: 'Pods',
    topology: t('Cluster topology'),
    events: t('Cluster events'),
    helm: t('nav.helm'),
  }
}

export function issueToPod(it: IssueItem): Pod {
  return {
    name: it.name,
    namespace: it.namespace ?? '',
    status: it.reason || 'Pending',
    ready: 0,
    total: 0,
    restarts: 0,
    node: '',
    ip: '',
    age: it.since,
    containers: it.containers ?? [],
    ownerKind: '',
    ownerName: '',
    reason: it.reason,
    deletedAt: '',
    finalizers: [],
  }
}

export const ov = (v?: number) => (v === undefined ? '—' : v.toLocaleString('pt-BR'))
