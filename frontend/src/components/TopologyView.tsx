import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Background, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Boxes, Layers, Network } from 'lucide-react'
import { api, type TopoNode } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

const KIND_ICON = { deployment: Layers, service: Network, pod: Boxes }
const COLUMN_X = { deployment: 20, pod: 360, service: 720 }

function statusTone(status: string): string {
  const s = status.toLowerCase()
  if (['running', 'available', 'succeeded'].includes(s)) return 'var(--ok)'
  if (['pending', 'progressing', 'containercreating', 'terminating'].includes(s)) return 'var(--warn)'
  if (['failed', 'error', 'crashloopbackoff'].includes(s)) return 'var(--err)'
  return 'var(--muted-foreground)'
}

// Custom graph node: a compact card with kind icon, name and a status dot.
function ResourceNode({ data }: NodeProps) {
  const d = data as unknown as TopoNode
  const Icon = KIND_ICON[d.kind]
  return (
    <div
      className={cn(
        'flex items-center gap-2 rounded-lg border bg-card px-3 py-2 shadow-lg',
        d.kind === 'deployment' && 'border-primary/50',
        d.kind === 'service' && 'border-[color:var(--ok)]/40',
      )}
    >
      <Handle type="target" position={Position.Left} className="!size-1.5 !border-0 !bg-border" />
      <Icon
        className={cn('size-4 shrink-0', d.kind === 'deployment' ? 'text-primary' : d.kind === 'service' ? 'text-[color:var(--ok)]' : 'text-muted-foreground')}
      />
      <div className="min-w-0">
        <div className="max-w-52 truncate text-xs font-medium">{d.name}</div>
        <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
          <span className="size-1.5 rounded-full" style={{ background: statusTone(d.status) }} />
          {d.status}
        </div>
      </div>
      <Handle type="source" position={Position.Right} className="!size-1.5 !border-0 !bg-border" />
    </div>
  )
}

const nodeTypes = { resource: ResourceNode }

export function TopologyView({ ctx, ns }: { ctx: string; ns: string }) {
  const t = useT()
  const q = useQuery({
    queryKey: ['topology', ctx, ns],
    queryFn: () => api.topology(ctx, ns),
    enabled: !!ctx && !!ns,
  })

  const { nodes, edges } = useMemo(() => {
    const g = q.data
    if (!g) return { nodes: [] as Node[], edges: [] as Edge[] }
    const counters = { deployment: 0, pod: 0, service: 0 }
    const nodes: Node[] = g.nodes.map((n) => {
      const row = counters[n.kind]++
      return {
        id: n.id,
        type: 'resource',
        position: { x: COLUMN_X[n.kind], y: row * 64 },
        data: n as unknown as Record<string, unknown>,
      }
    })
    const edges: Edge[] = g.edges.map((e, i) => ({
      id: `e${i}`,
      source: e.source,
      target: e.target,
      animated: true,
      style: { stroke: 'var(--border)', strokeWidth: 1.5 },
    }))
    return { nodes, edges }
  }, [q.data])

  if (!ns)
    return (
      <div className="flex h-[60vh] items-center justify-center rounded-2xl border bg-card/40 text-center text-sm text-muted-foreground">
        {t('Select a namespace at the top to view the topology.')}
      </div>
    )
  if (q.isLoading) return <div className="flex h-[60vh] items-center justify-center text-sm text-muted-foreground">{t('Loading topology...')}</div>
  if (q.isError) return <div className="flex h-[60vh] items-center justify-center text-sm text-[color:var(--err)]">{(q.error as Error).message}</div>

  return (
    <div className="h-[calc(100vh-11rem)] overflow-hidden rounded-2xl border bg-card/40">
      <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} fitView proOptions={{ hideAttribution: true }} minZoom={0.2} colorMode="dark">
        <Background gap={20} color="var(--border)" />
      </ReactFlow>
    </div>
  )
}
