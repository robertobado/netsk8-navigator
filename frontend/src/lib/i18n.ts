import { setAppPrefs, useAppPrefs } from './preferences'

// Light i18n layer. English is the source language everywhere in the code —
// backend labels, JSX literals, comments — so every literal string doubles as
// its own translation key by default (falls back to itself when untranslated).
// The `pt` dict below is the only translation table; anything not yet keyed
// there just renders as English. The active language lives in app
// preferences, so it persists and syncs like every other setting.

type Dict = Record<string, string>

const pt: Dict = {
  // --- app chrome (pre-existing semantic keys) ---
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

  // --- generic / shared ---
  'Loading...': 'Carregando...',
  'Loading details...': 'Carregando detalhes...',
  'Loading pods...': 'Carregando pods...',
  'Loading events...': 'Carregando eventos...',
  'Loading manifest...': 'Carregando manifest...',
  'Loading editor…': 'Carregando editor…',
  'Loading topology...': 'Carregando topologia...',
  'Loading YAML...': 'Carregando YAML...',
  'loading…': 'carregando…',
  Error: 'Erro',
  Age: 'Idade',
  'Open details': 'Abrir detalhes',
  'Try again': 'Tentar novamente',
  current: 'atual',
  live: 'ao vivo',
  connecting: 'conectando',
  reconnecting: 'reconectando',
  Previous: 'Anterior',
  Next: 'Próximo',
  Back: 'Voltar',
  Details: 'Detalhes',
  Detail: 'Detalhe',
  Confirm: 'Confirmar',
  Cancel: 'Cancelar',
  Apply: 'Aplicar',
  Discard: 'Descartar',
  Copy: 'Copiar',
  Copied: 'Copiado',
  Clear: 'Limpar',
  Pin: 'Fixar',
  Unpin: 'Desafixar',
  pinned: 'fixado',
  pin: 'fixar',
  'Resume carousel': 'Retomar carrossel',

  // --- DataTable ---
  'Filter column': 'Filtrar coluna',
  'No values': 'Sem valores',
  'Clear filter': 'Limpar filtro',
  'Filter...': 'Filtrar...',
  'No items to display.': 'Nenhum item para exibir.',

  // --- DetailView / ResourceDrawer / CustomResourceView ---
  'Controlled by': 'Controlado por',
  'Open owner': 'Abrir dono',
  Images: 'Imagens',
  Data: 'Dados',
  Conditions: 'Condições',
  'No pods.': 'Nenhum pod.',
  'Open pod details': 'Abrir detalhes do pod',
  'Hide value': 'Ocultar valor',
  'Reveal value': 'Revelar valor',
  Hide: 'Ocultar',
  Reveal: 'Revelar',
  'not ready': 'não pronto',

  // --- EventsView / EventsPanel ---
  'Could not load events for this cluster.': 'Não foi possível carregar os eventos deste cluster.',
  'The connection to the Kubernetes API failed — expired credential or no permission to list events. Renew the cluster login (e.g. AWS credentials) and try again.':
    'A conexão com a API do Kubernetes falhou — credencial expirada ou sem permissão para listar eventos. Renove o login do cluster (ex.: credenciais AWS) e tente de novo.',
  'The Kubernetes API did not respond to the event listing.': 'A API do Kubernetes não respondeu à listagem de eventos.',
  All: 'Todos',
  'Filter by reason, object, or message...': 'Filtrar por motivo, objeto ou mensagem...',
  'No warning events found.': 'Nenhum evento de warning encontrado.',
  'No events found.': 'Nenhum evento encontrado.',
  'Showing the': 'Mostrando os',
  'most recent events out of': 'eventos mais recentes de',
  'Narrow with the filter or search to see the rest.': 'Refine com o filtro ou a busca para ver os demais.',
  '{d} ago': 'há {d}',
  'No recent events for this pod.': 'Nenhum evento recente para este pod.',

  // --- ContextSwitcher ---
  'Select cluster': 'Selecionar cluster',
  'contexts available': 'contextos disponíveis',
  'Search cluster...': 'Buscar cluster...',
  'No context found.': 'Nenhum contexto encontrado.',

  // --- TerminalPanel ---
  'Connecting to container...': 'Conectando ao container...',
  'session closed': 'sessão encerrada',
  'connection error': 'erro de conexão',

  // --- IssueCarousel / MetricsSection carousels ---
  'no detail': 'sem detalhe',
  'Pin this item': 'Fixar neste item',
  'Pin to this node': 'Fixar neste nó',
  'Not-ready nodes': 'Nodes não prontos',

  // --- UsageGauge ---
  'no limit': 'sem limite',
  Memory: 'Memória',
  'Sort by % utilization': 'Ordenar por % de utilização',
  'Sort by absolute value': 'Ordenar por valor absoluto',

  // --- resourceCells ---
  'Not yet bound to a PersistentVolume.': 'Ainda não vinculado a um PersistentVolume.',
  No: 'Não',
  Yes: 'Sim',
  'Mounted by': 'Montado por',
  'volume referenced, no mountPath': 'volume referenciado, sem mountPath',

  // --- MetricsControls ---
  'Hide metrics': 'Ocultar métricas',
  'Refresh every': 'Atualizar a cada',

  // --- CommandPalette ---
  'Type a command or search...': 'Digite um comando ou busque...',
  'No results.': 'Nenhum resultado.',
  Navigate: 'Navegar',
  'Switch cluster': 'Trocar cluster',

  // --- ResourceView ---
  'Could not load': 'Não foi possível carregar',
  'The connection to the Kubernetes API failed — expired credential or no permission. Renew the cluster login (e.g. AWS credentials) and try again.':
    'A conexão com a API do Kubernetes falhou — credencial expirada ou sem permissão. Renove o login do cluster (ex.: credenciais AWS) e tente de novo.',
  'The Kubernetes API did not respond to this listing.': 'A API do Kubernetes não respondeu a esta listagem.',
  'Revision history': 'Histórico de revisões',

  // --- MetricsSection ---
  Metrics: 'Métricas',
  instant: 'instantâneo',
  of: 'de',
  allocatable: 'alocáveis',
  'no limit set': 'sem limite definido',
  'No data in this period': 'Sem dados no período',
  Utilization: 'Utilização',

  // --- App.tsx ---
  'Cluster overview': 'Visão geral do cluster',
  'Cluster topology': 'Topologia do cluster',
  'Cluster events': 'Eventos do cluster',
  Resource: 'Recurso',

  // --- PodsTable ---
  'Termination problems': 'Problemas no término',
  'Pending finalizers': 'Finalizers pendentes',
  Active: 'Ativo',
  Suspended: 'Suspenso',
  'No pods to display.': 'Nenhum pod para exibir.',

  // --- ManifestPanel / ManifestPanelLazy ---
  'read-only': 'somente leitura',
  'Copy YAML': 'Copiar YAML',
  'Applied to cluster': 'Aplicado ao cluster',
  'Apply to the live cluster?': 'Aplicar no cluster ao vivo?',

  // --- TopologyView ---
  'Select a namespace at the top to view the topology.': 'Selecione um namespace no topo para visualizar a topologia.',

  // --- Loader ---
  'Netsk8 Navigator loading': 'Netsk8 Navigator carregando',

  // --- LogsPanel ---
  'Search logs...': 'Buscar nos logs...',
  'Line wrap': 'Quebra de linha',
  'Newest first': 'Mais recentes primeiro',
  'No line matches the filter.': 'Nenhuma linha corresponde ao filtro.',
  'Waiting for logs...': 'Aguardando logs...',

  // --- ResourceExpansions ---
  'View {kind} details': 'Ver detalhes do {kind}',
  'Backend pods': 'Pods de backend',
  "No pods match this service's selector.": 'Nenhum pod corresponde ao selector deste service.',
  'No active pods for this workload.': 'Nenhum pod ativo para este workload.',
  'Consumed by': 'Consumido por',
  'No pods consume this resource.': 'Nenhum pod consome este recurso.',
  'View service account details': 'Ver detalhes do service account',
  'No binding references this SA and no pod uses it.': 'Nenhum binding referencia esta SA e nenhum pod a usa.',
  'Pods using this SA': 'Pods usando esta SA',
  'View node details': 'Ver detalhes do node',
  'No pods scheduled on this node.': 'Nenhum pod agendado neste node.',
  'Workloads on this node': 'Workloads no node',
  'Standalone pods': 'Pods avulsos',
  'View namespace details': 'Ver detalhes do namespace',
  'No resources in this namespace.': 'Nenhum recurso neste namespace.',
  'Resources in this namespace': 'Recursos no namespace',

  // --- CustomResourceView ---
  'No {kind} to display.': 'Nenhum {kind} para exibir.',

  // --- backend-sourced resource detail labels/titles/values (detail.go, crd.go) ---
  'Rules (verbs → resources)': 'Regras (verbos → recursos)',
  'Metrics (current / target)': 'Métricas (atual / alvo)',
  'Egress (outbound)': 'Egress (saída)',
  'Ingress (inbound)': 'Ingress (entrada)',
  'Usage / limit': 'Uso / limite',
  'Global default': 'Padrão global',
  'Binary data': 'Dados binários',
  'Volume expansion': 'Expansão de volume',
  'Current healthy': 'Saudáveis atuais',
  'Desired healthy': 'Saudáveis desejados',
  'Disruptions allowed': 'Disrupções permitidas',
  'Expected pods': 'Pods esperados',
  'Last run': 'Última execução',
  'Internal IP': 'IP interno',
  Strategy: 'Estratégia',
  Configuration: 'Configuração',
  Scheduling: 'Agendamento',
  Description: 'Descrição',
  Infrastructure: 'Infraestrutura',
  Addresses: 'Endereços',
  Parameters: 'Parâmetros',
  Allocatable: 'Alocável',
  Capacity: 'Capacidade',
  Architecture: 'Arquitetura',
  Schedulable: 'Agendável',
  System: 'Sistema',
  Subjects: 'Sujeitos',
  Healthy: 'Saudáveis',
  Replicas: 'Réplicas',
  Ready: 'Prontos',
  Keys: 'Chaves',
  Mounted: 'Montado',
  Criteria: 'Critério',
  Preemption: 'Preempção',
  Region: 'Região',
  Type: 'Tipo',
  Class: 'Classe',
  State: 'Estado',
  Phase: 'Fase',
  Source: 'Fonte',
  Modes: 'Modos',
  Default: 'Padrão',
  Value: 'Valor',
  Zone: 'Zona',
  Min: 'Mín',
  Max: 'Máx',
  Network: 'Rede',
  Governance: 'Governança',
  no: 'não',
  yes: 'sim',
  ready: 'pronto',
  all: 'todos',
  'inherits from pod': 'herda do pod',
  'any source/destination': 'qualquer origem/destino',
  'all ports': 'todas as portas',
  'Active pods': 'Pods ativos',
  'Active jobs': 'Jobs ativos',
  'Active finalizers': 'Finalizers ativos',
  Rule: 'Regra',

  // --- resources.tsx table headers ---
  Name: 'Nome',
  Disruptions: 'Disrupções',
  'Target pods': 'Pods alvo',
  Rules: 'Regras',
  Version: 'Versão',
  Access: 'Acesso',
  Types: 'Tipos',
  Target: 'Alvo',
  'PersistentVolumes in this class': 'PersistentVolumes desta classe',
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

// Compound backend-sourced free text mixes translatable connector words with
// dynamic data (K8s selectors, CIDRs) that can't be a whole-string dict key —
// e.g. "from any source/destination → all ports". Applied only as a
// last-resort fallback, and only when translating (never in English mode).
const PT_PHRASES: ReadonlyArray<[string, string]> = [
  ['any source/destination', 'qualquer origem/destino'],
  ['all ports', 'todas as portas'],
]

/**
 * Translate against the active language. English is the source language, so a
 * miss in `dict` falls straight back to the literal (never to the `pt` table
 * unless `dict` already *is* `pt`) — otherwise English mode would render
 * Portuguese for anything not explicitly re-added to the `en` table.
 */
export function useT(): TFunc {
  const lang = useAppPrefs().language
  const dict = DICTS[lang] ?? pt
  return (key, fallback) => {
    if (dict[key] !== undefined) return dict[key]

    // "<word> <number>" — e.g. backend-sourced "Rule 3": translate the static
    // prefix, keep the interpolated number as-is.
    const m = /^(.+) (\d+)$/.exec(key)
    if (m && dict[m[1]] !== undefined) return `${dict[m[1]]} ${m[2]}`

    if (dict === pt) {
      let s = key.replace(/^from /, 'de ').replace(/^to /, 'para ')
      for (const [from, to] of PT_PHRASES) s = s.split(from).join(to)
      if (s !== key) return s
    }

    return fallback ?? key
  }
}

/** "{duration} ago" reads as "há {duration}" in PT — word order, not just word
 * choice, so it needs a template rather than a plain key lookup. */
export function tAgo(t: TFunc, duration: string): string {
  return t('{d} ago', '{d} ago').replace('{d}', duration)
}

/** Generic `{name}`-style template translation for strings whose word order
 * differs between languages (e.g. "View {kind} details" / "Ver detalhes do
 * {kind}") — a plain key lookup can't reorder the interpolated part. */
export function tf(t: TFunc, key: string, vars: Record<string, string>): string {
  let s = t(key, key)
  for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(v)
  return s
}

export function setLanguage(code: string) {
  setAppPrefs({ language: code })
}
