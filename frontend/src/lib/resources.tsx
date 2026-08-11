import type { ReactNode } from 'react'
import { legacyCreateColumnHelper as createColumnHelper, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import {
  Boxes,
  Cable,
  Container,
  CopyPlus,
  Cpu,
  Database,
  Disc3,
  FileCog,
  Flag,
  Gauge,
  HardDrive,
  HeartPulse,
  KeyRound,
  Layers,
  Layers3,
  Link,
  Link2,
  Lock,
  LockKeyhole,
  Network,
  Play,
  Route,
  Ruler,
  Scale,
  Server,
  ShieldCheck,
  Signpost,
  Timer,
  UserCog,
  type LucideIcon,
} from 'lucide-react'
import type {
  ConfigMap,
  CronJob,
  DaemonSet,
  Deployment,
  EndpointSlice,
  HPA,
  Ingress,
  IngressClass,
  Job,
  LimitRange,
  ManifestKind,
  Namespace,
  NetworkPolicy,
  NodeRow,
  PDB,
  PriorityClass,
  PV,
  PVC,
  ReplicaSet,
  ResourceQuota,
  Role,
  RoleBinding,
  RuntimeClass,
  Secret,
  ServiceAccountRow,
  Service,
  StatefulSet,
  StorageClass,
} from '@/lib/api'
import { age, cn } from '@/lib/utils'
import { StatusBadge } from '@/components/StatusBadge'
import { CronJobStateCell, DeploymentStatus, JobStatus, MountedCell, PVCStatusCell, PVChildRow } from '@/components/resourceCells'

// The declarative resource catalog: one entry per standard resource drives both
// the sidebar nav and the generic table (columns, filters, detail kind). Adding a
// resource is one entry here — the backend already lists it generically.

type NavGroup = 'Workloads' | 'Network' | 'Config' | 'Storage' | 'RBAC' | 'Governance' | 'Cluster'

// Resources that keep revision history (e.g. ReplicaSets): old revisions are
// hidden by default and grouped by their controller, with a toggle to reveal them.
export interface ResourceHistory {
  isOld: (row: Record<string, unknown>) => boolean
  groupKey: (row: Record<string, unknown>) => string
  revision: (row: Record<string, unknown>) => number
}

// A parent→children relationship (e.g. StorageClass → its PVs): fetches the
// child resource, shows a per-row count column, and expands the row to list the
// matching children (each clickable to open its detail).
export interface ResourceExpand {
  resource: string // child plural resource to fetch
  manifest: ManifestKind // child kind for the detail drawer
  countHeader: string // header of the injected count column
  title: string // heading shown above the expanded child list
  parentKey: (parent: Record<string, unknown>) => string
  childKey: (child: Record<string, unknown>) => string
  renderChild: (child: Record<string, unknown>) => ReactNode
}

export interface ResourceDef {
  key: string // view key + nav id (e.g. "deployments")
  label: string
  icon: LucideIcon
  group: NavGroup
  resource: string // plural, for GET /resources/{resource}
  manifest: ManifestKind // slug for detail/manifest drawer
  facets: string[]
  columns: ColumnDef<never, unknown>[]
  usage?: boolean // aggregate CPU/mem gauges (deployments)
  history?: ResourceHistory
  clusterScoped?: boolean // no namespace (Namespaces, Nodes) — hide the ns filter
  expand?: ResourceExpand // row expands to list related children (StorageClass → PVs)
  // Bespoke row expansion with its own data join, keyed by a discriminator the
  // view resolves to a component: Node → workloads+pods, Namespace → resources,
  // workload-pods → this workload's/service's pods, serviceaccount → its
  // bindings+pods, consumers → pods consuming this ConfigMap/Secret.
  customExpand?: 'node' | 'namespace' | 'workload-pods' | 'serviceaccount' | 'consumers'
}

// --- shared cell renderers ---
const mono = (v: string) => <span className="font-mono text-xs text-muted-foreground">{v || '—'}</span>
const muted = (v: string) => <span className="text-muted-foreground">{v || '—'}</span>
const ageCell = (v: string) => <span className="font-mono text-sm text-muted-foreground tabular-nums">{age(v)}</span>
const ageSort = <T extends { age: string }>(a: { original: T }, b: { original: T }) => new Date(a.original.age).getTime() - new Date(b.original.age).getTime()

const nameCell = (c: { getValue: () => string }) => <span className="font-medium">{c.getValue()}</span>

// Colored "ready/desired" (green when all ready, amber otherwise).
const readyCell = (c: { getValue: () => string }) => {
  const [r, t] = c.getValue().split('/').map(Number)
  const ok = r === t && t > 0
  return <span className={cn('font-mono text-sm tabular-nums', ok ? 'text-[color:var(--ok)]' : 'text-[color:var(--warn)]')}>{c.getValue()}</span>
}

const dc = createColumnHelper<Deployment>()
const deploymentCols = [
  dc.accessor('name', { header: 'Name', cell: nameCell }),
  dc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  dc.accessor('ready', {
    header: 'Ready',
    cell: (c) => {
      const [r, t] = c.getValue().split('/').map(Number)
      const ok = r === t && t > 0
      return <span className={cn('font-mono text-sm tabular-nums', ok ? 'text-[color:var(--ok)]' : 'text-[color:var(--warn)]')}>{c.getValue()}</span>
    },
  }),
  dc.accessor('upToDate', { header: 'Up-to-date', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  dc.accessor('available', { header: 'Available', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  dc.accessor('status', { header: 'Status', cell: (c) => <DeploymentStatus status={c.getValue()} /> }),
  dc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const sc = createColumnHelper<Service>()
const serviceCols = [
  sc.accessor('name', { header: 'Name', cell: nameCell }),
  sc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  sc.accessor('type', { header: 'Type', cell: (c) => <StatusBadge status={c.getValue()} /> }),
  sc.accessor('clusterIP', { header: 'Cluster IP', cell: (c) => mono(c.getValue()) }),
  sc.accessor('externalIP', { header: 'External', cell: (c) => mono(c.getValue()) }),
  sc.accessor('ports', { header: 'Ports', cell: (c) => mono(c.getValue()) }),
  sc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const ic = createColumnHelper<Ingress>()
const ingressCols = [
  ic.accessor('name', { header: 'Name', cell: nameCell }),
  ic.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  ic.accessor('class', { header: 'Class', cell: (c) => muted(c.getValue()) }),
  ic.accessor('hosts', { header: 'Hosts', cell: (c) => <span className="text-sm">{c.getValue() || '—'}</span> }),
  ic.accessor('address', { header: 'Address', cell: (c) => mono(c.getValue()) }),
  ic.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const cc = createColumnHelper<ConfigMap>()
const configMapCols = [
  cc.accessor('name', { header: 'Name', cell: nameCell }),
  cc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  cc.accessor('keys', { header: 'Keys', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  cc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const stc = createColumnHelper<StatefulSet>()
const statefulSetCols = [
  stc.accessor('name', { header: 'Name', cell: nameCell }),
  stc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  stc.accessor('ready', { header: 'Ready', cell: readyCell }),
  stc.accessor('service', { header: 'Service', cell: (c) => muted(c.getValue()) }),
  stc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const dsc = createColumnHelper<DaemonSet>()
const daemonSetCols = [
  dsc.accessor('name', { header: 'Name', cell: nameCell }),
  dsc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  dsc.accessor('ready', { header: 'Ready', cell: readyCell }),
  dsc.accessor('upToDate', { header: 'Up-to-date', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  dsc.accessor('available', { header: 'Available', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  dsc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const rsc = createColumnHelper<ReplicaSet>()
const replicaSetCols = [
  rsc.accessor('name', { header: 'Name', cell: nameCell }),
  rsc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  rsc.accessor('ready', { header: 'Ready', cell: readyCell }),
  rsc.accessor((r) => (r.ownerKind ? `${r.ownerKind}/${r.ownerName}` : '—'), { id: 'controlledBy', header: 'Controlled By', cell: (c) => muted(c.getValue()) }),
  rsc.accessor('revision', {
    header: 'Rev.',
    cell: (c) => {
      const rev = c.getValue()
      const current = c.row.original.current
      return (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-mono text-xs text-muted-foreground tabular-nums">{rev || '—'}</span>
          {current && <span className="rounded-full bg-[color:var(--ok)]/12 px-1.5 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">atual</span>}
        </span>
      )
    },
  }),
  rsc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const jc = createColumnHelper<Job>()
const jobCols = [
  jc.accessor('name', { header: 'Name', cell: nameCell }),
  jc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  jc.accessor('completions', { header: 'Completions', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  jc.accessor('status', { header: 'Status', cell: (c) => <JobStatus status={c.getValue()} /> }),
  jc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const cjc = createColumnHelper<CronJob>()
const cronJobCols = [
  cjc.accessor('name', { header: 'Name', cell: nameCell }),
  cjc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  cjc.accessor('schedule', { header: 'Schedule', cell: (c) => mono(c.getValue()) }),
  cjc.accessor((r) => (r.suspend ? 'Suspended' : 'Active'), {
    id: 'suspend',
    header: 'State',
    cell: (c) => <CronJobStateCell value={c.getValue()} />,
  }),
  cjc.accessor('lastSchedule', {
    header: 'Last run',
    cell: (c) => <span className="font-mono text-sm text-muted-foreground tabular-nums">{c.getValue() ? age(c.getValue()) : '—'}</span>,
  }),
  cjc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

// Secret values are never listed — only the key count and type.
const secc = createColumnHelper<Secret>()
const secretCols = [
  secc.accessor('name', { header: 'Name', cell: nameCell }),
  secc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  secc.accessor('type', { header: 'Type', cell: (c) => mono(c.getValue()) }),
  secc.accessor('keys', { header: 'Keys', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  secc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

// Cluster-scoped — no namespace column.
const nsc = createColumnHelper<Namespace>()
const namespaceCols = [
  nsc.accessor('name', { header: 'Name', cell: nameCell }),
  nsc.accessor('status', { header: 'Status', cell: (c) => <StatusBadge status={c.getValue()} /> }),
  nsc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const ndc = createColumnHelper<NodeRow>()
const nodeCols = [
  ndc.accessor('name', { header: 'Name', cell: nameCell }),
  ndc.accessor('status', { header: 'Status', cell: (c) => <StatusBadge status={c.getValue()} /> }),
  ndc.accessor('roles', { header: 'Roles', cell: (c) => muted(c.getValue()) }),
  ndc.accessor('version', { header: 'Version', cell: (c) => mono(c.getValue()) }),
  ndc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const pvcc = createColumnHelper<PVC>()
const pvcCols = [
  pvcc.accessor('name', { header: 'Name', cell: nameCell }),
  pvcc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  pvcc.accessor('status', { header: 'Status', cell: (c) => <PVCStatusCell pvc={c.row.original} /> }),
  pvcc.accessor((r) => r.mountedBy?.length ?? 0, {
    id: 'mounted',
    header: 'Mounted',
    sortDescFirst: true,
    cell: (c) => <MountedCell pvc={c.row.original} />,
  }),
  pvcc.accessor('capacity', { header: 'Capacity', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue() || '—'}</span> }),
  pvcc.accessor('accessModes', { header: 'Access', cell: (c) => mono(c.getValue()) }),
  pvcc.accessor('storageClass', { header: 'StorageClass', cell: (c) => muted(c.getValue()) }),
  pvcc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const pvc = createColumnHelper<PV>()
const pvCols = [
  pvc.accessor('name', { header: 'Name', cell: nameCell }),
  pvc.accessor('status', { header: 'Status', cell: (c) => <StatusBadge status={c.getValue()} /> }),
  pvc.accessor('capacity', { header: 'Capacity', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue() || '—'}</span> }),
  pvc.accessor('accessModes', { header: 'Access', cell: (c) => mono(c.getValue()) }),
  pvc.accessor('reclaim', { header: 'Reclaim', cell: (c) => muted(c.getValue()) }),
  pvc.accessor('claim', { header: 'Claim', cell: (c) => muted(c.getValue()) }),
  pvc.accessor('storageClass', { header: 'StorageClass', cell: (c) => muted(c.getValue()) }),
  pvc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const scc = createColumnHelper<StorageClass>()
const storageClassCols = [
  scc.accessor('name', {
    header: 'Name',
    cell: (c) => (
      <span className="inline-flex items-center gap-1.5">
        <span className="font-medium">{c.getValue()}</span>
        {c.row.original.default && (
          <span className="rounded-full bg-[color:var(--ok)]/12 px-1.5 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">default</span>
        )}
      </span>
    ),
  }),
  scc.accessor('provisioner', { header: 'Provisioner', cell: (c) => mono(c.getValue()) }),
  scc.accessor('reclaim', { header: 'Reclaim', cell: (c) => muted(c.getValue()) }),
  scc.accessor('binding', { header: 'Binding', cell: (c) => muted(c.getValue()) }),
  scc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const hpac = createColumnHelper<HPA>()
const hpaCols = [
  hpac.accessor('name', { header: 'Name', cell: nameCell }),
  hpac.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  hpac.accessor('reference', { header: 'Target', cell: (c) => mono(c.getValue()) }),
  hpac.accessor('minPods', { header: 'Min', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  hpac.accessor('maxPods', { header: 'Max', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  hpac.accessor('replicas', { header: 'Replicas', cell: (c) => <span className="font-mono text-sm tabular-nums">{c.getValue()}</span> }),
  hpac.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const esc = createColumnHelper<EndpointSlice>()
const endpointSliceCols = [
  esc.accessor('name', { header: 'Name', cell: nameCell }),
  esc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  esc.accessor('service', { header: 'Service', cell: (c) => muted(c.getValue()) }),
  esc.accessor('addressType', { header: 'Type', cell: (c) => mono(c.getValue()) }),
  esc.accessor((r) => `${r.ready}/${r.total}`, { id: 'ready', header: 'Ready', cell: (c) => readyCell({ getValue: () => c.getValue() as string }) }),
  esc.accessor('ports', { header: 'Ports', cell: (c) => mono(c.getValue()) }),
  esc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const npc = createColumnHelper<NetworkPolicy>()
const networkPolicyCols = [
  npc.accessor('name', { header: 'Name', cell: nameCell }),
  npc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  npc.accessor('podSelector', { header: 'Target pods', cell: (c) => mono(c.getValue()) }),
  npc.accessor('policyTypes', { header: 'Types', cell: (c) => muted(c.getValue()) }),
  npc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const icc = createColumnHelper<IngressClass>()
const ingressClassCols = [
  icc.accessor('name', {
    header: 'Name',
    cell: (c) => (
      <span className="inline-flex items-center gap-1.5">
        <span className="font-medium">{c.getValue()}</span>
        {c.row.original.default && (
          <span className="rounded-full bg-[color:var(--ok)]/12 px-1.5 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">default</span>
        )}
      </span>
    ),
  }),
  icc.accessor('controller', { header: 'Controller', cell: (c) => mono(c.getValue()) }),
  icc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const num = (c: { getValue: () => unknown }) => <span className="font-mono text-sm tabular-nums">{c.getValue() as number}</span>

const sac = createColumnHelper<ServiceAccountRow>()
const serviceAccountCols = [
  sac.accessor('name', { header: 'Name', cell: nameCell }),
  sac.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  sac.accessor('secrets', { header: 'Secrets', cell: num }),
  sac.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const rlc = createColumnHelper<Role>()
const roleCols = [
  rlc.accessor('name', { header: 'Name', cell: nameCell }),
  rlc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  rlc.accessor('rules', { header: 'Rules', cell: num }),
  rlc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]
const clusterRoleCols = [
  rlc.accessor('name', { header: 'Name', cell: nameCell }),
  rlc.accessor('rules', { header: 'Rules', cell: num }),
  rlc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const rbc = createColumnHelper<RoleBinding>()
const subjectsCell = (c: { getValue: () => unknown }) => mono(((c.getValue() as string[] | null) ?? []).join(', '))
const roleBindingCols = [
  rbc.accessor('name', { header: 'Name', cell: nameCell }),
  rbc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  rbc.accessor('role', { header: 'Role', cell: (c) => mono(c.getValue()) }),
  rbc.accessor('subjects', { header: 'Subjects', cell: subjectsCell }),
  rbc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]
const clusterRoleBindingCols = [
  rbc.accessor('name', { header: 'Name', cell: nameCell }),
  rbc.accessor('role', { header: 'Role', cell: (c) => mono(c.getValue()) }),
  rbc.accessor('subjects', { header: 'Subjects', cell: subjectsCell }),
  rbc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const rqc = createColumnHelper<ResourceQuota>()
const resourceQuotaCols = [
  rqc.accessor('name', { header: 'Name', cell: nameCell }),
  rqc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  rqc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const lrc = createColumnHelper<LimitRange>()
const limitRangeCols = [
  lrc.accessor('name', { header: 'Name', cell: nameCell }),
  lrc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  lrc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const pdbc = createColumnHelper<PDB>()
const pdbCols = [
  pdbc.accessor('name', { header: 'Name', cell: nameCell }),
  pdbc.accessor('namespace', { header: 'Namespace', cell: (c) => muted(c.getValue()) }),
  pdbc.accessor('criteria', { header: 'Criteria', cell: (c) => mono(c.getValue()) }),
  pdbc.accessor((r) => r.current - r.desired, {
    id: 'healthy',
    header: 'Healthy',
    // A PDB is healthy when current >= desired (enough healthy pods for the budget),
    // not on strict equality — desired is a minimum, and is often 0 or below current.
    cell: (c) => {
      const { current, desired } = c.row.original
      const ok = current >= desired
      return (
        <span className={cn('font-mono text-sm tabular-nums', ok ? 'text-[color:var(--ok)]' : 'text-[color:var(--warn)]')}>
          {current}/{desired}
        </span>
      )
    },
  }),
  pdbc.accessor('allowed', {
    header: 'Disruptions',
    cell: (c) => (
      <span className={cn('font-mono text-sm tabular-nums', (c.getValue() as number) > 0 ? 'text-[color:var(--ok)]' : 'text-muted-foreground')}>
        {c.getValue() as number}
      </span>
    ),
  }),
  pdbc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const prc = createColumnHelper<PriorityClass>()
const priorityClassCols = [
  prc.accessor('name', {
    header: 'Name',
    cell: (c) => (
      <span className="inline-flex items-center gap-1.5">
        <span className="font-medium">{c.getValue()}</span>
        {c.row.original.globalDefault && (
          <span className="rounded-full bg-[color:var(--ok)]/12 px-1.5 py-0.5 text-[10px] font-medium text-[color:var(--ok)]">default</span>
        )}
      </span>
    ),
  }),
  prc.accessor('value', { header: 'Value', cell: num, sortDescFirst: true }),
  prc.accessor('preemption', { header: 'Preemption', cell: (c) => muted(c.getValue()) }),
  prc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

const ruc = createColumnHelper<RuntimeClass>()
const runtimeClassCols = [
  ruc.accessor('name', { header: 'Name', cell: nameCell }),
  ruc.accessor('handler', { header: 'Handler', cell: (c) => mono(c.getValue()) }),
  ruc.accessor('age', { header: 'Age', cell: (c) => ageCell(c.getValue()), sortFn: ageSort }),
] as ColumnDef<never, unknown>[]

export const RESOURCES: ResourceDef[] = [
  {
    key: 'deployments',
    label: 'Deployments',
    icon: Layers,
    group: 'Workloads',
    resource: 'deployments',
    manifest: 'deployment',
    facets: ['namespace', 'status'],
    columns: deploymentCols,
    usage: true,
    customExpand: 'workload-pods',
  },
  {
    key: 'statefulsets',
    label: 'StatefulSets',
    icon: Database,
    group: 'Workloads',
    resource: 'statefulsets',
    manifest: 'statefulset',
    facets: ['namespace'],
    columns: statefulSetCols,
    customExpand: 'workload-pods',
  },
  {
    key: 'daemonsets',
    label: 'DaemonSets',
    icon: Server,
    group: 'Workloads',
    resource: 'daemonsets',
    manifest: 'daemonset',
    facets: ['namespace'],
    columns: daemonSetCols,
    customExpand: 'workload-pods',
  },
  {
    key: 'replicasets',
    label: 'ReplicaSets',
    icon: CopyPlus,
    group: 'Workloads',
    resource: 'replicasets',
    manifest: 'replicaset',
    facets: ['namespace'],
    columns: replicaSetCols,
    history: {
      isOld: (r) => !r.current,
      groupKey: (r) => `${r.ownerKind as string}/${r.ownerName as string}`,
      revision: (r) => Number(r.revision) || 0,
    },
  },
  { key: 'jobs', label: 'Jobs', icon: Play, group: 'Workloads', resource: 'jobs', manifest: 'job', facets: ['namespace', 'status'], columns: jobCols },
  {
    key: 'cronjobs',
    label: 'CronJobs',
    icon: Timer,
    group: 'Workloads',
    resource: 'cronjobs',
    manifest: 'cronjob',
    facets: ['namespace'],
    columns: cronJobCols,
  },
  {
    key: 'services',
    label: 'Services',
    icon: Network,
    group: 'Network',
    resource: 'services',
    manifest: 'service',
    facets: ['namespace', 'type'],
    columns: serviceCols,
    customExpand: 'workload-pods',
  },
  {
    key: 'ingresses',
    label: 'Ingresses',
    icon: Route,
    group: 'Network',
    resource: 'ingresses',
    manifest: 'ingress',
    facets: ['namespace', 'class'],
    columns: ingressCols,
  },
  {
    key: 'ingressclasses',
    label: 'IngressClasses',
    icon: Signpost,
    group: 'Network',
    resource: 'ingressclasses',
    manifest: 'ingressclass',
    facets: ['controller'],
    columns: ingressClassCols,
    clusterScoped: true,
  },
  {
    key: 'endpointslices',
    label: 'EndpointSlices',
    icon: Cable,
    group: 'Network',
    resource: 'endpointslices',
    manifest: 'endpointslice',
    facets: ['namespace', 'addressType'],
    columns: endpointSliceCols,
  },
  {
    key: 'networkpolicies',
    label: 'NetworkPolicies',
    icon: ShieldCheck,
    group: 'Network',
    resource: 'networkpolicies',
    manifest: 'networkpolicy',
    facets: ['namespace'],
    columns: networkPolicyCols,
  },
  {
    key: 'configmaps',
    label: 'ConfigMaps',
    icon: FileCog,
    group: 'Config',
    resource: 'configmaps',
    manifest: 'configmap',
    facets: ['namespace'],
    columns: configMapCols,
    customExpand: 'consumers',
  },
  {
    key: 'secrets',
    label: 'Secrets',
    icon: KeyRound,
    group: 'Config',
    resource: 'secrets',
    manifest: 'secret',
    facets: ['namespace', 'type'],
    columns: secretCols,
    customExpand: 'consumers',
  },
  { key: 'hpas', label: 'HPAs', icon: Gauge, group: 'Config', resource: 'horizontalpodautoscalers', manifest: 'hpa', facets: ['namespace'], columns: hpaCols },
  {
    key: 'pvcs',
    label: 'PersistentVolumeClaims',
    icon: HardDrive,
    group: 'Storage',
    resource: 'persistentvolumeclaims',
    manifest: 'pvc',
    facets: ['namespace', 'status', 'storageClass'],
    columns: pvcCols,
  },
  {
    key: 'pvs',
    label: 'PersistentVolumes',
    icon: Disc3,
    group: 'Storage',
    resource: 'persistentvolumes',
    manifest: 'pv',
    facets: ['status', 'storageClass'],
    columns: pvCols,
    clusterScoped: true,
  },
  {
    key: 'storageclasses',
    label: 'StorageClasses',
    icon: Layers3,
    group: 'Storage',
    resource: 'storageclasses',
    manifest: 'storageclass',
    facets: ['provisioner'],
    columns: storageClassCols,
    clusterScoped: true,
    expand: {
      resource: 'persistentvolumes',
      manifest: 'pv',
      countHeader: 'PVs',
      title: 'PersistentVolumes in this class',
      parentKey: (sc) => sc.name as string,
      childKey: (pv) => pv.storageClass as string,
      renderChild: (pv) => <PVChildRow pv={pv as unknown as PV} />,
    },
  },
  {
    key: 'namespaces',
    label: 'Namespaces',
    icon: Boxes,
    group: 'Cluster',
    resource: 'namespaces',
    manifest: 'namespace',
    facets: ['status'],
    columns: namespaceCols,
    clusterScoped: true,
    customExpand: 'namespace',
  },
  {
    key: 'nodes',
    label: 'Nodes',
    icon: Cpu,
    group: 'Cluster',
    resource: 'nodes',
    manifest: 'node',
    facets: ['status'],
    columns: nodeCols,
    clusterScoped: true,
    customExpand: 'node',
  },
  {
    key: 'priorityclasses',
    label: 'PriorityClasses',
    icon: Flag,
    group: 'Cluster',
    resource: 'priorityclasses',
    manifest: 'priorityclass',
    facets: [],
    columns: priorityClassCols,
    clusterScoped: true,
  },
  {
    key: 'runtimeclasses',
    label: 'RuntimeClasses',
    icon: Container,
    group: 'Cluster',
    resource: 'runtimeclasses',
    manifest: 'runtimeclass',
    facets: [],
    columns: runtimeClassCols,
    clusterScoped: true,
  },
  {
    key: 'serviceaccounts',
    label: 'ServiceAccounts',
    icon: UserCog,
    group: 'RBAC',
    resource: 'serviceaccounts',
    manifest: 'serviceaccount',
    facets: ['namespace'],
    columns: serviceAccountCols,
    customExpand: 'serviceaccount',
  },
  { key: 'roles', label: 'Roles', icon: Lock, group: 'RBAC', resource: 'roles', manifest: 'role', facets: ['namespace'], columns: roleCols },
  {
    key: 'rolebindings',
    label: 'RoleBindings',
    icon: Link2,
    group: 'RBAC',
    resource: 'rolebindings',
    manifest: 'rolebinding',
    facets: ['namespace'],
    columns: roleBindingCols,
  },
  {
    key: 'clusterroles',
    label: 'ClusterRoles',
    icon: LockKeyhole,
    group: 'RBAC',
    resource: 'clusterroles',
    manifest: 'clusterrole',
    facets: [],
    columns: clusterRoleCols,
    clusterScoped: true,
  },
  {
    key: 'clusterrolebindings',
    label: 'ClusterRoleBindings',
    icon: Link,
    group: 'RBAC',
    resource: 'clusterrolebindings',
    manifest: 'clusterrolebinding',
    facets: [],
    columns: clusterRoleBindingCols,
    clusterScoped: true,
  },
  {
    key: 'resourcequotas',
    label: 'ResourceQuotas',
    icon: Scale,
    group: 'Governance',
    resource: 'resourcequotas',
    manifest: 'resourcequota',
    facets: ['namespace'],
    columns: resourceQuotaCols,
  },
  {
    key: 'limitranges',
    label: 'LimitRanges',
    icon: Ruler,
    group: 'Governance',
    resource: 'limitranges',
    manifest: 'limitrange',
    facets: ['namespace'],
    columns: limitRangeCols,
  },
  {
    key: 'poddisruptionbudgets',
    label: 'PodDisruptionBudgets',
    icon: HeartPulse,
    group: 'Governance',
    resource: 'poddisruptionbudgets',
    manifest: 'poddisruptionbudget',
    facets: ['namespace'],
    columns: pdbCols,
  },
]

export function resourceByKey(key: string): ResourceDef | undefined {
  return RESOURCES.find((r) => r.key === key)
}
