import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DeploymentStatus } from './resourceCells'

describe('DeploymentStatus', () => {
  it('renders Available as a green pill', () => {
    const { container } = render(<DeploymentStatus status="Available" />)
    expect(screen.getByText('Available')).toBeInTheDocument()
    expect(container.querySelector('.lucide-circle-check')).toBeInTheDocument()
  })

  it('renders Progressing as a blue pill', () => {
    const { container } = render(<DeploymentStatus status="Progressing" />)
    expect(screen.getByText('Progressing')).toBeInTheDocument()
    expect(container.querySelector('.text-\\[\\#38bdf8\\]')).toBeInTheDocument()
  })

  it('renders "Scaled to 0" as a muted pill', () => {
    const { container } = render(<DeploymentStatus status="Scaled to 0" />)
    expect(screen.getByText('Scaled to 0')).toBeInTheDocument()
    expect(container.querySelector('.lucide-circle-off')).toBeInTheDocument()
  })

  // Regression: a Deployment whose old ReplicaSet is fully Ready (so its
  // replica counts look identical to a healthy rollout) but whose Progressing
  // condition has gone False (backend/internal/kube/convert.go's
  // deploymentStatus) used to fall through to a neutral/unknown badge here —
  // indistinguishable from any other unrecognized string, easy to miss in a
  // busy list. It must render as an error, not a neutral one.
  it('renders an unrecognized status (a failed/stuck rollout condition reason) as an error pill, not a neutral badge', () => {
    const { container } = render(<DeploymentStatus status="ProgressDeadlineExceeded" />)
    expect(screen.getByText('ProgressDeadlineExceeded')).toBeInTheDocument()
    expect(container.querySelector('.lucide-triangle-alert')).toBeInTheDocument()
    expect(container.querySelector('[class*="--err"]')).toBeInTheDocument()
  })
})
