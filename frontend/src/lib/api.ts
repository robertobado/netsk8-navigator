// Typed client for the netsk8s-navigator Go backend. All calls go through the
// Vite dev proxy (/api -> :8080), so relative URLs work in dev and prod.

export interface Health {
  status: string
  kubeconfig: string
  demo: boolean
  version: string
  authEnabled: boolean
}

export interface UpdateCheck {
  available: boolean
  latest?: string
  url?: string
}

export interface ContextInfo {
  name: string
  cluster: string
  user: string
  namespace: string
  server: string
  current: boolean
}

export interface Overview {
  nodes: number
  readyNodes: number
  pods: number
  namespaces: number
  running: number
  pending: number
  failed: number
}

export interface Pod {
  name: string
  namespace: string
  status: string
  ready: number
  total: number
  restarts: number
  node: string
  ip: string
  age: string
  containers: string[]
  ownerKind: string
  ownerName: string
  reason: string
  deletedAt: string
  finalizers: string[]
}

export interface NamespaceInfo {
  name: string
  status: string
  age: string
}

export interface PendingInfo {
  since: string
  reason: string
  message: string
}

export interface EventView {
  type: string // Normal | Warning
  reason: string
  message: string
  count: number
  first: string
  last: string
  source: string
  // Involved object — populated only by the cluster-wide events list.
  objectKind?: string
  objectNamespace?: string
  objectName?: string
}

export interface IssueItem {
  kind: 'pod' | 'node'
  namespace?: string
  name: string
  since: string // when it entered the state (RFC3339)
  reason: string
  message: string
  containers?: string[]
}
export interface Issues {
  pending: IssueItem[]
  failed: IssueItem[]
  nodesNotReady: IssueItem[]
}

// The minimal addressing shape shared by every generic-CRD endpoint.
export interface CRDRef {
  group: string
  version: string
  resource: string
}

// A route-like CRD (Gateway API, Traefik, Istio, Contour) served by the cluster.
export interface RouteKind extends CRDRef {
  kind: string
  namespaced: boolean
  label: string
  order: number
}
// Any CRD the cluster serves, from the generic browser (no allowlist) — same
// shape as RouteKind minus the curated "Network" nav ordering.
export interface CRDKind extends CRDRef {
  kind: string
  namespaced: boolean
  label: string
}
export interface CRDItem {
  name: string
  namespace: string
  age: string
  hosts: string
  refs: string
}

export interface Deployment {
  name: string
  namespace: string
  ready: string
  upToDate: number
  available: number
  status: string
  age: string
}

export interface Service {
  name: string
  namespace: string
  type: string
  clusterIP: string
  externalIP: string
  ports: string
  age: string
}

export interface Ingress {
  name: string
  namespace: string
  class: string
  hosts: string
  address: string
  age: string
}

export interface ConfigMap {
  name: string
  namespace: string
  keys: number
  age: string
}

export interface Secret {
  name: string
  namespace: string
  type: string
  keys: number
  age: string
}

// Cluster-scoped resources (no namespace).
export interface Namespace {
  name: string
  status: string
  age: string
}
export interface NodeRow {
  name: string
  status: string
  roles: string
  version: string
  age: string
}

export interface PVCMountPoint {
  container: string
  path: string
}
export interface PVCMount {
  pod: string
  mounts: PVCMountPoint[] | null
}
export interface PVC {
  name: string
  namespace: string
  status: string
  volume: string
  capacity: string
  accessModes: string
  storageClass: string
  mountedBy: PVCMount[] | null // pods (same namespace) currently mounting the claim
  age: string
}
export interface PV {
  name: string
  capacity: string
  accessModes: string
  reclaim: string
  status: string
  claim: string
  storageClass: string
  age: string
}
export interface StorageClass {
  name: string
  provisioner: string
  reclaim: string
  binding: string
  default: boolean
  age: string
}
export interface HPA {
  name: string
  namespace: string
  reference: string
  minPods: number
  maxPods: number
  replicas: number
  age: string
}
export interface EndpointSlice {
  name: string
  namespace: string
  service: string
  addressType: string
  ready: number
  total: number
  ports: string
  age: string
}
export interface NetworkPolicy {
  name: string
  namespace: string
  podSelector: string
  policyTypes: string
  age: string
}
export interface IngressClass {
  name: string
  controller: string
  default: boolean
  age: string
}
export interface ServiceAccountRow {
  name: string
  namespace: string
  secrets: number
  age: string
}
export interface Role {
  name: string
  namespace: string // empty for ClusterRole
  rules: number
  age: string
}
export interface RoleBinding {
  name: string
  namespace: string // empty for ClusterRoleBinding
  role: string
  subjects: string[] | null // formatted subjects (SA "ns/name", user:/group:)
  age: string
}
export interface ResourceQuota {
  name: string
  namespace: string
  age: string
}
export interface LimitRange {
  name: string
  namespace: string
  age: string
}
export interface PDB {
  name: string
  namespace: string
  criteria: string
  current: number
  desired: number
  allowed: number
  age: string
}
export interface PriorityClass {
  name: string
  value: number
  globalDefault: boolean
  preemption: string
  age: string
}
export interface RuntimeClass {
  name: string
  handler: string
  age: string
}

export interface StatefulSet {
  name: string
  namespace: string
  ready: string
  service: string
  age: string
}
export interface ReplicaSet {
  name: string
  namespace: string
  ready: string
  ownerKind: string
  ownerName: string
  revision: string
  current: boolean
  age: string
}
export interface DaemonSet {
  name: string
  namespace: string
  ready: string
  upToDate: number
  available: number
  age: string
}
export interface Job {
  name: string
  namespace: string
  completions: string
  status: string
  age: string
}
export interface CronJob {
  name: string
  namespace: string
  schedule: string
  suspend: boolean
  active: number
  lastSchedule: string
  age: string
}

// Node row expansion: pods on the node, grouped by their top-level workload.
export interface NodeWorkloadGroup {
  kind: string // Deployment | StatefulSet | DaemonSet | Job | Pod (standalone) | …
  slug: string // manifest slug for the detail drawer ("" when not openable)
  namespace: string
  name: string
  pods: Pod[]
}
// Namespace row expansion: a namespace's resources grouped by type.
export interface NsNameRef {
  name: string
  namespace: string
}
export interface NamespaceGroup {
  kind: string
  slug: ManifestKind
  items: NsNameRef[]
}

// ServiceAccount row expansion: the bindings that grant it + pods running as it.
export interface BindingRef {
  kind: string
  slug: ManifestKind
  namespace: string
  name: string
}
export interface SAUsage {
  bindings: BindingRef[]
  pods: Pod[]
  permissions: DetailKV[] // effective, deduped, from every bound Role/ClusterRole
}

export interface Monitoring {
  available: boolean // Prometheus-compatible time-series backend
  kind?: string
  namespace?: string
  service?: string
  port?: number
  metricsServer?: boolean // metrics-server → instantaneous gauges
}
export interface Gauge {
  used: number
  request?: number // pod: summed container requests
  limit?: number // pod: summed container limits (0 = none set)
  total: number // effective ceiling (limit→request for pods; allocatable for node/cluster); 0 = unbounded
  unit: string
}
export interface Usage {
  available: boolean
  cpu?: Gauge
  memory?: Gauge
}
export interface PodUsageEntry {
  cpu: Gauge
  memory: Gauge
}
export interface PodsUsage {
  available: boolean
  items?: Record<string, PodUsageEntry> // keyed by "<namespace>/<name>"
}
export interface NodeUsageItem {
  name: string
  cpu: Gauge
  memory: Gauge
}
export interface NodesUsage {
  available: boolean
  items?: NodeUsageItem[] // sorted by peak utilization, desc
}
export interface MetricPoint {
  t: number
  v: number
}
export interface MetricSeries {
  points: MetricPoint[]
  unit: string
}
export interface Metrics {
  available: boolean
  source?: string
  cpu?: MetricSeries
  memory?: MetricSeries
}

export interface TopoNode {
  id: string
  kind: 'pod' | 'deployment' | 'service'
  name: string
  status: string
}
export interface TopoEdge {
  source: string
  target: string
}
export interface TopoGraph {
  nodes: TopoNode[]
  edges: TopoEdge[]
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`/api${path}`)
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      /* ignore non-JSON error bodies */
    }
    throw new Error(msg)
  }
  return res.json() as Promise<T>
}

// Context names can be EKS ARNs containing slashes — encode each path segment.
const enc = (s: string) => encodeURIComponent(s)

const nsQuery = (namespace?: string) => (namespace ? `?namespace=${enc(namespace)}` : '')

export const api = {
  health: () => get<Health>('/health'),
  updateCheck: () => get<UpdateCheck>('/update-check'),
  contexts: () => get<ContextInfo[]>('/contexts'),
  mcpToken: () => get<{ token: string }>('/mcp/token'),
  overview: (ctx: string) => get<Overview>(`/contexts/${enc(ctx)}/overview`),
  namespaces: (ctx: string) => get<NamespaceInfo[]>(`/contexts/${enc(ctx)}/namespaces`),
  pods: (ctx: string, ns?: string) => get<Pod[]>(`/contexts/${enc(ctx)}/pods${nsQuery(ns)}`),
  podPending: (ctx: string, ns: string, name: string) => get<PendingInfo>(`/contexts/${enc(ctx)}/pods/${enc(ns)}/${enc(name)}/pending`),
  events: (ctx: string, ns: string, name: string, kind?: string) =>
    get<EventView[]>(`/contexts/${enc(ctx)}/events/${enc(ns)}/${enc(name)}${kind ? `?kind=${enc(kind)}` : ''}`),
  allEvents: (ctx: string, ns?: string) => get<EventView[]>(`/contexts/${enc(ctx)}/events${nsQuery(ns)}`),
  workloadPods: (ctx: string, kind: ManifestKind, ns: string, name: string) => get<Pod[]>(`/contexts/${enc(ctx)}/pods-of/${kind}/${enc(ns)}/${enc(name)}`),
  nodeWorkloads: (ctx: string, node: string) => get<NodeWorkloadGroup[]>(`/contexts/${enc(ctx)}/node-workloads/${enc(node)}`),
  namespaceSummary: (ctx: string, ns: string) => get<NamespaceGroup[]>(`/contexts/${enc(ctx)}/namespace-summary/${enc(ns)}`),
  serviceAccountUsage: (ctx: string, ns: string, name: string) => get<SAUsage>(`/contexts/${enc(ctx)}/serviceaccount-usage/${enc(ns)}/${enc(name)}`),
  consumers: (ctx: string, kind: 'configmap' | 'secret', ns: string, name: string) =>
    get<Pod[]>(`/contexts/${enc(ctx)}/consumers/${kind}/${enc(ns)}/${enc(name)}`),
  // Catalog-driven list: any standard resource by its plural name (backend catalog).
  list: <T = unknown>(ctx: string, resource: string, ns?: string) => get<T[]>(`/contexts/${enc(ctx)}/resources/${resource}${nsQuery(ns)}`),
  topology: (ctx: string, ns: string) => get<TopoGraph>(`/contexts/${enc(ctx)}/topology?namespace=${enc(ns)}`),
  monitoring: (ctx: string) => get<Monitoring>(`/contexts/${enc(ctx)}/monitoring`),
  issues: (ctx: string) => get<Issues>(`/contexts/${enc(ctx)}/issues`),
  routeKinds: (ctx: string) => get<RouteKind[]>(`/contexts/${enc(ctx)}/routekinds`),
  crdKinds: (ctx: string) => get<CRDKind[]>(`/contexts/${enc(ctx)}/crdkinds`),
  crdList: (ctx: string, rk: CRDRef, ns?: string) => get<CRDItem[]>(`/contexts/${enc(ctx)}/crd/${rk.group}/${rk.version}/${rk.resource}${nsQuery(ns)}`),
  podsUsage: (ctx: string, ns?: string) => get<PodsUsage>(`/contexts/${enc(ctx)}/podusage${nsQuery(ns)}`),
  nodesUsage: (ctx: string) => get<NodesUsage>(`/contexts/${enc(ctx)}/nodeusage`),
  deploymentsUsage: (ctx: string, ns?: string) => get<PodsUsage>(`/contexts/${enc(ctx)}/deploymentusage${nsQuery(ns)}`),
  usage: (ctx: string, scope: 'cluster' | 'pod' | 'node', params?: { namespace?: string; name?: string }) => {
    const qs = new URLSearchParams()
    if (params?.namespace) qs.set('namespace', params.namespace)
    if (params?.name) qs.set('name', params.name)
    const s = qs.toString()
    return get<Usage>(`/contexts/${enc(ctx)}/usage/${scope}${s ? `?${s}` : ''}`)
  },
  metrics: (ctx: string, scope: 'cluster' | 'pod' | 'node', params?: { namespace?: string; name?: string; range?: string }) => {
    const q = new URLSearchParams()
    if (params?.namespace) q.set('namespace', params.namespace)
    if (params?.name) q.set('name', params.name)
    if (params?.range) q.set('range', params.range)
    const qs = q.toString()
    return get<Metrics>(`/contexts/${enc(ctx)}/metrics/${scope}${qs ? `?${qs}` : ''}`)
  },
}

/** Manifest kind (singular) accepted by the manifest endpoint. */
export type ManifestKind =
  | 'pod'
  | 'deployment'
  | 'service'
  | 'ingress'
  | 'configmap'
  | 'replicaset'
  | 'statefulset'
  | 'daemonset'
  | 'job'
  | 'cronjob'
  | 'node'
  | 'namespace'
  | 'secret'
  | 'pvc'
  | 'pv'
  | 'storageclass'
  | 'hpa'
  | 'endpointslice'
  | 'networkpolicy'
  | 'ingressclass'
  | 'serviceaccount'
  | 'role'
  | 'clusterrole'
  | 'rolebinding'
  | 'clusterrolebinding'
  | 'resourcequota'
  | 'limitrange'
  | 'poddisruptionbudget'
  | 'priorityclass'
  | 'runtimeclass'

// Every k8s Kind we can open a drawer for → its manifest slug. Used to make an
// event's involved object clickable, and (as a superset) for owner links.
const KIND_TO_SLUG: Record<string, ManifestKind> = {
  Pod: 'pod',
  Deployment: 'deployment',
  Service: 'service',
  Ingress: 'ingress',
  ConfigMap: 'configmap',
  ReplicaSet: 'replicaset',
  StatefulSet: 'statefulset',
  DaemonSet: 'daemonset',
  Job: 'job',
  CronJob: 'cronjob',
  Node: 'node',
  Namespace: 'namespace',
  Secret: 'secret',
  PersistentVolumeClaim: 'pvc',
  PersistentVolume: 'pv',
  StorageClass: 'storageclass',
  HorizontalPodAutoscaler: 'hpa',
  EndpointSlice: 'endpointslice',
  NetworkPolicy: 'networkpolicy',
  IngressClass: 'ingressclass',
  ServiceAccount: 'serviceaccount',
  Role: 'role',
  ClusterRole: 'clusterrole',
  RoleBinding: 'rolebinding',
  ClusterRoleBinding: 'clusterrolebinding',
  ResourceQuota: 'resourcequota',
  LimitRange: 'limitrange',
  PodDisruptionBudget: 'poddisruptionbudget',
  PriorityClass: 'priorityclass',
  RuntimeClass: 'runtimeclass',
}

/** Maps a k8s ownerReference/controller/involvedObject Kind to our manifest slug. */
export function kindToSlug(kind: string): ManifestKind | null {
  return KIND_TO_SLUG[kind] ?? null
}

// Per-kind metadata needed to generate a blank manifest template: the k8s Kind
// name (also doubles as SLUG_TO_KIND, the inverse of KIND_TO_SLUG) and the
// apiVersion the cluster serves it under. Kept as one table instead of two
// separate maps — with 29 kinds, two maps sharing the same key order read as
// duplicated code to static analysis (and to a human skimming the diff).
const MANIFEST_META: Record<ManifestKind, { kind: string; apiVersion: string }> = {
  pod: { kind: 'Pod', apiVersion: 'v1' },
  deployment: { kind: 'Deployment', apiVersion: 'apps/v1' },
  service: { kind: 'Service', apiVersion: 'v1' },
  ingress: { kind: 'Ingress', apiVersion: 'networking.k8s.io/v1' },
  configmap: { kind: 'ConfigMap', apiVersion: 'v1' },
  replicaset: { kind: 'ReplicaSet', apiVersion: 'apps/v1' },
  statefulset: { kind: 'StatefulSet', apiVersion: 'apps/v1' },
  daemonset: { kind: 'DaemonSet', apiVersion: 'apps/v1' },
  job: { kind: 'Job', apiVersion: 'batch/v1' },
  cronjob: { kind: 'CronJob', apiVersion: 'batch/v1' },
  node: { kind: 'Node', apiVersion: 'v1' },
  namespace: { kind: 'Namespace', apiVersion: 'v1' },
  secret: { kind: 'Secret', apiVersion: 'v1' },
  pvc: { kind: 'PersistentVolumeClaim', apiVersion: 'v1' },
  pv: { kind: 'PersistentVolume', apiVersion: 'v1' },
  storageclass: { kind: 'StorageClass', apiVersion: 'storage.k8s.io/v1' },
  hpa: { kind: 'HorizontalPodAutoscaler', apiVersion: 'autoscaling/v2' },
  endpointslice: { kind: 'EndpointSlice', apiVersion: 'discovery.k8s.io/v1' },
  networkpolicy: { kind: 'NetworkPolicy', apiVersion: 'networking.k8s.io/v1' },
  ingressclass: { kind: 'IngressClass', apiVersion: 'networking.k8s.io/v1' },
  serviceaccount: { kind: 'ServiceAccount', apiVersion: 'v1' },
  role: { kind: 'Role', apiVersion: 'rbac.authorization.k8s.io/v1' },
  clusterrole: { kind: 'ClusterRole', apiVersion: 'rbac.authorization.k8s.io/v1' },
  rolebinding: { kind: 'RoleBinding', apiVersion: 'rbac.authorization.k8s.io/v1' },
  clusterrolebinding: { kind: 'ClusterRoleBinding', apiVersion: 'rbac.authorization.k8s.io/v1' },
  resourcequota: { kind: 'ResourceQuota', apiVersion: 'v1' },
  limitrange: { kind: 'LimitRange', apiVersion: 'v1' },
  poddisruptionbudget: { kind: 'PodDisruptionBudget', apiVersion: 'policy/v1' },
  priorityclass: { kind: 'PriorityClass', apiVersion: 'scheduling.k8s.io/v1' },
  runtimeclass: { kind: 'RuntimeClass', apiVersion: 'node.k8s.io/v1' },
}

/** The inverse of KIND_TO_SLUG — the k8s Kind for a manifest slug, e.g. for event filters and blank-template generation. */
export const SLUG_TO_KIND: Record<ManifestKind, string> = Object.fromEntries(
  (Object.keys(MANIFEST_META) as ManifestKind[]).map((slug) => [slug, MANIFEST_META[slug].kind]),
) as Record<ManifestKind, string>

/** Kinds the "New resource" dialog offers — excludes controller-managed (ReplicaSet, EndpointSlice) and non-user-created (Node) kinds. */
export const CREATABLE_KINDS = new Set<ManifestKind>([
  'deployment',
  'statefulset',
  'daemonset',
  'job',
  'cronjob',
  'service',
  'ingress',
  'configmap',
  'secret',
  'namespace',
  'pvc',
  'pv',
  'storageclass',
  'hpa',
  'networkpolicy',
  'ingressclass',
  'serviceaccount',
  'role',
  'clusterrole',
  'rolebinding',
  'clusterrolebinding',
  'resourcequota',
  'limitrange',
  'poddisruptionbudget',
  'priorityclass',
  'runtimeclass',
])

/** Blank starting-point YAML for the "New resource" dialog. */
export function blankManifestYAML(kind: ManifestKind, namespace: string, clusterScoped: boolean): string {
  const meta = clusterScoped ? `metadata:\n  name: \n` : `metadata:\n  name: \n  namespace: ${namespace || 'default'}\n`
  return `apiVersion: ${MANIFEST_META[kind].apiVersion}\nkind: ${MANIFEST_META[kind].kind}\n${meta}`
}

export interface DetailKV {
  label: string
  value: string
  // Populated only for generic CRD fields the backend can't show as a single
  // scalar: a simple array becomes chips, an object with only simple fields
  // becomes a nested grid, anything nested deeper becomes a read-only YAML
  // code block — see fieldRow in backend/internal/api/crd.go.
  chips?: string[]
  grid?: DetailKV[]
  code?: string
}
export interface DetailChip {
  label: string
  value: string
  tone: 'ok' | 'warn' | 'err' | 'muted'
}
export interface DetailSection {
  title: string
  items: DetailKV[]
}
export interface DetailRef {
  group: string
  kind: ManifestKind
  namespace: string
  name: string
  note?: string // optional secondary line (e.g. an endpoint's IP)
}
export interface DetailBlock {
  title: string
  body: string
  masked?: boolean // Secret values — hidden until the user reveals them
}
export interface PortView {
  name?: string
  port: string
  protocol?: string
  extra?: string
}
export interface DetailProblem {
  reason: string
  message: string
  tone: 'warn' | 'err'
}
export interface ResourceDetail {
  kind: string
  name: string
  namespace: string
  age: string
  ownerKind: string
  ownerName: string
  status: DetailChip[]
  problem?: DetailProblem | null
  sections: DetailSection[]
  selector: Record<string, string> | null
  images: DetailKV[]
  conditions: DetailChip[]
  labels: Record<string, string> | null
  refs: DetailRef[] | null
  blocks: DetailBlock[] | null
  hosts: string[] | null
  ports: PortView[] | null
  replicas?: number
  schedulable?: boolean // nodes only — false when cordoned
}

/** Kinds that have a structured detail view (others fall back to YAML only). */
export const KINDS_WITH_DETAIL = new Set<ManifestKind>([
  'pod',
  'node',
  'namespace',
  'secret',
  'pvc',
  'pv',
  'storageclass',
  'hpa',
  'endpointslice',
  'networkpolicy',
  'ingressclass',
  'serviceaccount',
  'role',
  'clusterrole',
  'rolebinding',
  'clusterrolebinding',
  'resourcequota',
  'limitrange',
  'poddisruptionbudget',
  'priorityclass',
  'runtimeclass',
  'deployment',
  'replicaset',
  'statefulset',
  'daemonset',
  'job',
  'cronjob',
  'service',
  'ingress',
  'configmap',
])

export function getDetail(ctx: string, kind: ManifestKind, namespace: string, name: string) {
  return get<ResourceDetail>(`/contexts/${enc(ctx)}/detail/${kind}/${enc(namespace || '-')}/${enc(name)}`)
}

export async function getManifest(ctx: string, kind: ManifestKind, namespace: string, name: string) {
  // Node is cluster-scoped; the endpoint still needs a namespace segment.
  const ns = namespace || '-'
  const r = await get<{ yaml: string }>(`/contexts/${enc(ctx)}/manifest/${kind}/${enc(ns)}/${enc(name)}`)
  return r.yaml
}

export function crdDetail(ctx: string, rk: CRDRef, namespace: string, name: string) {
  const ns = namespace || '-'
  return get<ResourceDetail>(`/contexts/${enc(ctx)}/crd/${rk.group}/${rk.version}/${rk.resource}/${enc(ns)}/${enc(name)}/detail`)
}

export async function crdManifest(ctx: string, rk: CRDRef, namespace: string, name: string) {
  const ns = namespace || '-'
  const r = await get<{ yaml: string }>(`/contexts/${enc(ctx)}/crd/${rk.group}/${rk.version}/${rk.resource}/${enc(ns)}/${enc(name)}/manifest`)
  return r.yaml
}

// PUT/DELETE counterparts to crdManifest — edit/delete an instance of ANY
// CRD, addressed by GVR rather than a manifest slug. Mirrors applyManifest/
// deleteResource exactly (same dry-run/error handling).
export async function crdApply(
  ctx: string,
  rk: CRDRef,
  namespace: string,
  name: string,
  yaml: string,
  opts?: { dryRun?: boolean },
): Promise<string | undefined> {
  const ns = namespace || '-'
  const qs = opts?.dryRun ? '?dryRun=true' : ''
  const res = await fetch(`/api/contexts/${enc(ctx)}/crd/${rk.group}/${rk.version}/${rk.resource}/${enc(ns)}/${enc(name)}${qs}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yaml }),
  })
  await throwIfError(res)
  if (!opts?.dryRun) return undefined
  const body = (await res.json()) as { yaml: string }
  return body.yaml
}

export async function crdDelete(ctx: string, rk: CRDRef, namespace: string, name: string) {
  const ns = namespace || '-'
  const res = await fetch(`/api/contexts/${enc(ctx)}/crd/${rk.group}/${rk.version}/${rk.resource}/${enc(ns)}/${enc(name)}`, { method: 'DELETE' })
  await throwIfError(res)
}

// A resource is addressed either by its catalog manifest slug (typed kinds,
// the ~30-entry closed set) or by a CRDRef (any CRD, including ones outside
// that catalog) — ManifestPanel/ResourceActions dispatch on which one they
// were given via the three helpers below, so both worlds share one UI.
export type ResourceRef = ManifestKind | CRDRef

export function getManifestRef(ctx: string, ref: ResourceRef, namespace: string, name: string) {
  return typeof ref === 'string' ? getManifest(ctx, ref, namespace, name) : crdManifest(ctx, ref, namespace, name)
}

export function applyManifestRef(ctx: string, ref: ResourceRef, namespace: string, name: string, yaml: string, opts?: { dryRun?: boolean }) {
  return typeof ref === 'string' ? applyManifest(ctx, ref, namespace, name, yaml, opts) : crdApply(ctx, ref, namespace, name, yaml, opts)
}

export function deleteResourceRef(ctx: string, ref: ResourceRef, namespace: string, name: string) {
  return typeof ref === 'string' ? deleteResource(ctx, ref, namespace, name) : crdDelete(ctx, ref, namespace, name)
}

/** Throws with the backend's {"error": "..."} message (falling back to the status line) when a response isn't ok. */
async function throwIfError(res: Response): Promise<void> {
  if (res.ok) return
  let msg = `${res.status} ${res.statusText}`
  try {
    const b = await res.json()
    if (b?.error) msg = b.error
  } catch {
    /* ignore non-JSON error bodies */
  }
  throw new Error(msg)
}

// dryRun asks the API server to validate + run admission/defaulting without
// persisting, returning the YAML it would have produced — used to preview a
// change (and surface validation errors) before applying for real.
export async function applyManifest(
  ctx: string,
  kind: ManifestKind,
  namespace: string,
  name: string,
  yaml: string,
  opts?: { dryRun?: boolean },
): Promise<string | undefined> {
  const qs = opts?.dryRun ? '?dryRun=true' : ''
  const res = await fetch(`/api/contexts/${enc(ctx)}/manifest/${kind}/${enc(namespace || '-')}/${enc(name)}${qs}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yaml }),
  })
  await throwIfError(res)
  if (!opts?.dryRun) return undefined
  const body = (await res.json()) as { yaml: string }
  return body.yaml
}

/** Kinds whose replica count can be changed without editing YAML. */
export const SCALABLE_KINDS = new Set<ManifestKind>(['deployment', 'statefulset', 'replicaset'])
/** Kinds that support a `kubectl rollout restart`-style pod template bump. */
export const RESTARTABLE_KINDS = new Set<ManifestKind>(['deployment', 'statefulset', 'daemonset'])

export async function deleteResource(ctx: string, kind: ManifestKind, namespace: string, name: string) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/manifest/${kind}/${enc(namespace || '-')}/${enc(name)}`, { method: 'DELETE' })
  await throwIfError(res)
}

export async function scaleResource(ctx: string, kind: ManifestKind, namespace: string, name: string, replicas: number) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/scale/${kind}/${enc(namespace || '-')}/${enc(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ replicas }),
  })
  await throwIfError(res)
}

/** Invalidates the current /mcp auth token and returns the newly generated one. */
export async function regenerateMCPToken(): Promise<{ token: string }> {
  const res = await fetch('/api/mcp/token/regenerate', { method: 'POST' })
  await throwIfError(res)
  return res.json()
}

export async function restartRollout(ctx: string, kind: ManifestKind, namespace: string, name: string) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/rollout-restart/${kind}/${enc(namespace || '-')}/${enc(name)}`, { method: 'POST' })
  await throwIfError(res)
}

/** Toggles a node's schedulability — `kubectl cordon`/`uncordon`. */
export async function cordonNode(ctx: string, name: string, cordon: boolean) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/cordon/${enc(name)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cordon }),
  })
  await throwIfError(res)
}

export interface CreatedResource {
  kind: string
  namespace: string
  name: string
}

// dryRun previews the object the API server would actually create (validation +
// defaulting) without persisting it — same idea as applyManifest's dry-run.
export async function createResource(ctx: string, yaml: string, opts?: { dryRun?: boolean }): Promise<CreatedResource | { yaml: string }> {
  const qs = opts?.dryRun ? '?dryRun=true' : ''
  const res = await fetch(`/api/contexts/${enc(ctx)}/create${qs}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yaml }),
  })
  await throwIfError(res)
  return res.json()
}

/** Kinds with revision history (undo) available — see docs/FEATURE_GAP_ANALYSIS.md for why StatefulSet/DaemonSet aren't included yet. */
export const HISTORY_KINDS = new Set<ManifestKind>(['deployment'])

export interface RevisionInfo {
  revision: number
  images: string[]
  createdAt: string
  current: boolean
}

export function rolloutHistory(ctx: string, kind: ManifestKind, namespace: string, name: string) {
  return get<RevisionInfo[]>(`/contexts/${enc(ctx)}/rollout-history/${kind}/${enc(namespace)}/${enc(name)}`)
}

export async function rolloutUndo(ctx: string, kind: ManifestKind, namespace: string, name: string, toRevision: number) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/rollout-undo/${kind}/${enc(namespace)}/${enc(name)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ toRevision }),
  })
  await throwIfError(res)
}

export interface PortForwardSession {
  id: string
  namespace: string
  pod: string
  port: number
  localPort: number
}

export async function startPortForward(ctx: string, namespace: string, name: string, port: number): Promise<{ id: string; localPort: number }> {
  const res = await fetch(`/api/contexts/${enc(ctx)}/portforward/${enc(namespace)}/${enc(name)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ port }),
  })
  await throwIfError(res)
  return res.json()
}

export async function stopPortForward(ctx: string, id: string) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/portforward/${enc(id)}`, { method: 'DELETE' })
  await throwIfError(res)
}

export function listPortForwards(ctx: string) {
  return get<PortForwardSession[]>(`/contexts/${enc(ctx)}/portforward`)
}

/** SSE URL for streaming a pod container's logs. */
export function logsURL(ctx: string, namespace: string, pod: string, container?: string) {
  const c = container ? `?container=${enc(container)}` : ''
  return `/api/contexts/${enc(ctx)}/pods/${enc(namespace)}/${enc(pod)}/logs${c}`
}

/** Kinds whose "Logs" tab aggregates every owned pod's log stream into one view. */
export const MULTI_LOG_KINDS = new Set<ManifestKind>(['deployment', 'statefulset', 'daemonset', 'replicaset', 'job'])

/** SSE URL for streaming every pod of a workload's logs, tagged with the source pod. */
export function workloadLogsURL(ctx: string, kind: ManifestKind, namespace: string, name: string, container?: string) {
  const c = container ? `?container=${enc(container)}` : ''
  return `/api/contexts/${enc(ctx)}/pods-of/${kind}/${enc(namespace)}/${enc(name)}/logs${c}`
}

/** WebSocket URL for an interactive exec session into a pod container. */
export function execURL(ctx: string, namespace: string, pod: string, container?: string) {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const c = container ? `?container=${enc(container)}` : ''
  return `${proto}://${window.location.host}/api/contexts/${enc(ctx)}/pods/${enc(namespace)}/${enc(pod)}/exec${c}`
}

// --- Helm --------------------------------------------------------------
// Releases are per-cluster (under /contexts/{ctx}/helm/...); repos and chart
// search are local to this machine (~/.config/helm), so those endpoints carry
// no context.

export interface HelmRelease {
  name: string
  namespace: string
  chart: string // "nginx-1.2.3"
  appVersion: string
  revision: number
  status: string
  updated: string
}
export interface HelmReleaseDetail extends HelmRelease {
  notes: string
  values: string // YAML
}
export interface HelmRepo {
  name: string
  url: string
}
export interface HelmChartSummary {
  repo: string
  name: string
  version: string
  appVersion: string
  description: string
}
export interface HelmChartDetail {
  versions: string[]
  defaultValues: string // YAML
  readme: string
}
export interface HelmInstallRequest {
  repo: string
  chart: string
  version: string
  releaseName: string
  namespace: string
  values: string // YAML
}

export function helmReleases(ctx: string, ns?: string) {
  return get<HelmRelease[]>(`/contexts/${enc(ctx)}/helm/releases${nsQuery(ns)}`)
}
export function helmReleaseStatus(ctx: string, ns: string, name: string) {
  return get<HelmReleaseDetail>(`/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}`)
}
export async function helmReleaseManifest(ctx: string, ns: string, name: string) {
  const r = await get<{ yaml: string }>(`/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}/manifest`)
  return r.yaml
}
export function helmReleaseHistory(ctx: string, ns: string, name: string) {
  return get<HelmRelease[]>(`/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}/history`)
}
export async function helmReleaseRollback(ctx: string, ns: string, name: string, revision: number) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}/rollback`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision }),
  })
  await throwIfError(res)
}
export async function helmReleaseUninstall(ctx: string, ns: string, name: string) {
  const res = await fetch(`/api/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}`, { method: 'DELETE' })
  await throwIfError(res)
}
export async function installHelmRelease(ctx: string, req: HelmInstallRequest): Promise<HelmRelease> {
  const res = await fetch(`/api/contexts/${enc(ctx)}/helm/releases`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  await throwIfError(res)
  return res.json()
}
export async function upgradeHelmRelease(ctx: string, ns: string, name: string, req: HelmInstallRequest): Promise<HelmRelease> {
  const res = await fetch(`/api/contexts/${enc(ctx)}/helm/releases/${enc(ns)}/${enc(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  await throwIfError(res)
  return res.json()
}

export function helmRepos() {
  return get<HelmRepo[]>('/helm/repos')
}
export async function addHelmRepo(name: string, url: string): Promise<HelmRepo> {
  const res = await fetch('/api/helm/repos', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, url }),
  })
  await throwIfError(res)
  return res.json()
}
export async function removeHelmRepo(name: string) {
  const res = await fetch(`/api/helm/repos/${enc(name)}`, { method: 'DELETE' })
  await throwIfError(res)
}
export async function refreshHelmRepo(name: string) {
  const res = await fetch(`/api/helm/repos/${enc(name)}/refresh`, { method: 'POST' })
  await throwIfError(res)
}
export function helmSearch(q: string) {
  return get<HelmChartSummary[]>(`/helm/search${q ? `?q=${enc(q)}` : ''}`)
}
export function helmChartDetail(repo: string, chart: string, version?: string) {
  return get<HelmChartDetail>(`/helm/charts/${enc(repo)}/${enc(chart)}${version ? `?version=${enc(version)}` : ''}`)
}
