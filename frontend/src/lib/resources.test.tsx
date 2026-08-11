import { describe, expect, it } from 'vitest'
import { fireEvent, render } from '@testing-library/react'
import { flexRender } from '@tanstack/react-table'
import { getCoreRowModel, useLegacyTable, type LegacyColumnDef as ColumnDef } from '@tanstack/react-table/legacy'
import { RESOURCES, resourceByKey } from './resources'

// One fabricated row carrying every field any resource's columns accessor
// reads, keyed by name — good enough to drive every cell renderer in
// RESOURCES without a real cluster or API mocking, since resources.tsx's
// column defs are declarative config with no data-fetching of their own.
const kitchenSinkRow: Record<string, unknown> = {
  name: 'demo',
  namespace: 'default',
  age: new Date(Date.now() - 3_600_000).toISOString(),
  ready: '2/2',
  upToDate: 2,
  available: 2,
  status: 'Running',
  type: 'ClusterIP',
  clusterIP: '10.0.0.1',
  externalIP: '-',
  ports: '80/TCP',
  class: 'nginx',
  hosts: 'example.com',
  address: '1.2.3.4',
  keys: 3,
  service: 'demo-svc',
  schedule: '*/5 * * * *',
  suspend: false,
  lastSchedule: new Date().toISOString(),
  completions: '1/1',
  ownerKind: 'Deployment',
  ownerName: 'demo',
  revision: '3',
  current: true,
  roles: 'control-plane',
  version: 'v1.30.0',
  volume: 'pv-1',
  mountedBy: [{ pod: 'web-1', mounts: [{ container: 'app', path: '/data' }] }],
  capacity: '10Gi',
  accessModes: 'RWO',
  storageClass: 'standard',
  reclaim: 'Delete',
  claim: 'demo-pvc',
  provisioner: 'csi.example.com',
  binding: 'Immediate',
  default: true,
  reference: 'Deployment/demo',
  minPods: 1,
  maxPods: 5,
  replicas: 2,
  addressType: 'IPv4',
  total: 2,
  podSelector: 'app=demo',
  policyTypes: 'Ingress',
  controller: 'k8s.io/ingress-nginx',
  secrets: 2,
  rules: 3,
  subjects: ['ServiceAccount:default/demo'],
  role: 'edit',
  criteria: 'minAvailable: 1',
  desired: 1,
  allowed: 1,
  globalDefault: false,
  value: 1000,
  preemption: 'PreemptLowerPriority',
  handler: 'runc',
}

const unboundPVCRow: Record<string, unknown> = { ...kitchenSinkRow, status: 'Pending', mountedBy: [] }

function TableHarness({ columns, data }: Readonly<{ columns: ColumnDef<never, unknown>[]; data: Record<string, unknown>[] }>) {
  const table = useLegacyTable({ data: data as never[], columns, getCoreRowModel: getCoreRowModel() })
  return (
    <table>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

describe('RESOURCES column cells', () => {
  for (const def of RESOURCES) {
    it(`${def.key} renders its columns against a representative row without throwing`, () => {
      const { container } = render(<TableHarness columns={def.columns} data={[kitchenSinkRow]} />)
      expect(container.querySelector('tbody tr')).toBeTruthy()
    })
  }

  it('pvcs renders the unbound (not-yet-Bound) and empty-mountedBy branches', () => {
    const pvcs = resourceByKey('pvcs')!
    const { container } = render(<TableHarness columns={pvcs.columns} data={[unboundPVCRow]} />)
    expect(container.querySelector('tbody tr')).toBeTruthy()
  })

  it('pvcs mounted-by hover bubble opens on mouse enter (StorageClass PV/claim hover, MountedCell pods list)', () => {
    const pvcs = resourceByKey('pvcs')!
    const { container } = render(<TableHarness columns={pvcs.columns} data={[kitchenSinkRow]} />)
    for (const span of container.querySelectorAll('span.inline-flex')) {
      fireEvent.mouseEnter(span)
    }
    expect(document.body.textContent).toContain('web-1')
  })
})

describe('resourceByKey', () => {
  it('finds a known resource', () => {
    expect(resourceByKey('deployments')?.label).toBe('Deployments')
  })
  it('returns undefined for an unknown key', () => {
    expect(resourceByKey('bogus')).toBeUndefined()
  })
})
