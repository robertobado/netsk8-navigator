import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DetailBody } from './DetailView'
import type { ResourceDetail } from '@/lib/api'

vi.mock('@/lib/i18n', () => ({ useT: () => (key: string) => key }))

function detail(sections: ResourceDetail['sections']): ResourceDetail {
  return {
    kind: 'Widget',
    name: 'w1',
    namespace: 'prod',
    age: '',
    ownerKind: '',
    ownerName: '',
    status: [],
    sections,
    selector: null,
    images: [],
    conditions: [],
    labels: null,
    refs: null,
    blocks: null,
    hosts: null,
    ports: null,
  }
}

describe('DetailBody — generic CRD field rendering', () => {
  it('renders a scalar field as a plain label/value row', () => {
    render(<DetailBody d={detail([{ title: 'Spec', items: [{ label: 'statusCode', value: '503' }] }])} ctx="c" kind="widget" namespace="prod" name="w1" />)
    expect(screen.getByText('statusCode')).toBeInTheDocument()
    expect(screen.getByText('503')).toBeInTheDocument()
  })

  it('renders a simple array field as chips instead of "N items"', () => {
    render(
      <DetailBody
        d={detail([{ title: 'Spec', items: [{ label: 'dnsNames', value: '', chips: ['example.com', 'www.example.com'] }] }])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('dnsNames')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByText('www.example.com')).toBeInTheDocument()
    expect(screen.queryByText(/items/)).not.toBeInTheDocument()
  })

  it('renders an object with only simple fields as a nested grid, not "{N fields}"', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [
              {
                label: 'privateKey',
                value: '',
                grid: [
                  { label: 'algorithm', value: 'ECDSA' },
                  { label: 'size', value: '256' },
                ],
              },
            ],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('privateKey')).toBeInTheDocument()
    expect(screen.getByText('algorithm')).toBeInTheDocument()
    expect(screen.getByText('ECDSA')).toBeInTheDocument()
    expect(screen.getByText('size')).toBeInTheDocument()
    expect(screen.getByText('256')).toBeInTheDocument()
    expect(screen.queryByText(/fields}/)).not.toBeInTheDocument()
  })

  it('renders a deeply-nested field as a read-only YAML code block, not "{N fields}"', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [{ label: 'directResponse', value: '', code: 'statusCode: 503\nbody:\n  inline: unavailable' }],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('directResponse')).toBeInTheDocument()
    expect(screen.getByText('yaml')).toBeInTheDocument()
    const code = screen.getByText((_, el) => el?.tagName === 'PRE' && !!el.textContent?.includes('statusCode: 503'))
    expect(code).toBeInTheDocument()
    expect(screen.queryByText(/fields}/)).not.toBeInTheDocument()
  })

  it('nested grid rows can themselves be chips (e.g. subject.organizations)', () => {
    render(
      <DetailBody
        d={detail([
          {
            title: 'Spec',
            items: [{ label: 'subject', value: '', grid: [{ label: 'organizations', value: '', chips: ['Netsk8 Inc'] }] }],
          },
        ])}
        ctx="c"
        kind="widget"
        namespace="prod"
        name="w1"
      />,
    )
    expect(screen.getByText('organizations')).toBeInTheDocument()
    expect(screen.getByText('Netsk8 Inc')).toBeInTheDocument()
  })
})

describe('DetailBody — pod problem banner', () => {
  it('renders the reason and message when the pod has a problem', () => {
    const d = detail([])
    d.problem = { reason: 'CrashLoopBackOff', message: 'back-off restarting failed container', tone: 'err' }
    render(<DetailBody d={d} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    expect(screen.getByText('back-off restarting failed container')).toBeInTheDocument()
  })

  it('falls back to a placeholder when the reason has no message', () => {
    const d = detail([])
    d.problem = { reason: 'Unschedulable', message: '', tone: 'warn' }
    render(<DetailBody d={d} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.getByText('Unschedulable')).toBeInTheDocument()
    expect(screen.getByText('no detail')).toBeInTheDocument()
  })

  it('renders no banner for a healthy pod', () => {
    render(<DetailBody d={detail([])} ctx="c" kind="widget" namespace="prod" name="web-1" />)
    expect(screen.queryByText('no detail')).not.toBeInTheDocument()
  })
})
