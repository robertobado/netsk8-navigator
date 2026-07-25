import { setAppPrefs, useAppPrefs } from './preferences'

// Light i18n layer. Strings are migrated to `t('key')` incrementally, view by
// view; anything not yet keyed keeps its literal text. The active language lives
// in app preferences, so it persists and syncs like every other setting.

type Dict = Record<string, string>

const pt: Dict = {
  'app.subtitle': 'Gerenciamento de Cluster',
  'app.allNamespaces': 'todos os namespaces',
  'ns.all': 'Todos os namespaces',
  'ns.search': 'Buscar namespace...',
  'ns.label': 'Namespace',
  'app.connecting': 'Conectando…',
  'app.ready': 'Pronto',
  'app.loadingClusters': 'Carregando clusters do kubeconfig...',
  'app.selectCluster': 'Selecione um cluster para começar.',
  'nav.overview': 'Visão geral',
  'nav.pods': 'Pods',
  'nav.topology': 'Topologia',
  'nav.events': 'Eventos',
  'group.workloads': 'Workloads',
  'group.rede': 'Rede',
  'group.config': 'Config',
  'group.storage': 'Armazenamento',
  'group.rbac': 'RBAC',
  'group.governanca': 'Governança',
  'group.cluster': 'Cluster',
  'controls.metrics': 'Métricas',
  'controls.background': 'Fundo animado',
  'controls.language': 'Idioma',
}

const en: Dict = {
  'app.subtitle': 'Cluster Management',
  'app.allNamespaces': 'all namespaces',
  'ns.all': 'All namespaces',
  'ns.search': 'Search namespace...',
  'ns.label': 'Namespace',
  'app.connecting': 'Connecting…',
  'app.ready': 'Ready',
  'app.loadingClusters': 'Loading clusters from kubeconfig...',
  'app.selectCluster': 'Select a cluster to get started.',
  'nav.overview': 'Overview',
  'nav.pods': 'Pods',
  'nav.topology': 'Topology',
  'nav.events': 'Events',
  'group.workloads': 'Workloads',
  'group.rede': 'Network',
  'group.config': 'Config',
  'group.storage': 'Storage',
  'group.rbac': 'RBAC',
  'group.governanca': 'Governance',
  'group.cluster': 'Cluster',
  'controls.metrics': 'Metrics',
  'controls.background': 'Animated background',
  'controls.language': 'Language',
}

const DICTS: Record<string, Dict> = { 'pt-BR': pt, en }

export const LANGUAGES: ReadonlyArray<{ code: string; label: string }> = [
  { code: 'pt-BR', label: 'PT' },
  { code: 'en', label: 'EN' },
]

export type TFunc = (key: string, fallback?: string) => string

/** Translate against the active language, falling back to pt-BR, then the key. */
export function useT(): TFunc {
  const lang = useAppPrefs().language
  const dict = DICTS[lang] ?? pt
  return (key, fallback) => dict[key] ?? pt[key] ?? fallback ?? key
}

export function setLanguage(code: string) {
  setAppPrefs({ language: code })
}
