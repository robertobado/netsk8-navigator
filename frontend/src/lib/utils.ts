import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merge Tailwind classes with conditional logic (the shadcn/ui `cn` helper). */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Turn an ISO creation timestamp into a compact k8s-style age (e.g. "3d", "2h"). */
export function age(iso?: string): string {
  if (!iso) return '—'
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '—'
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h`
  const days = Math.floor(hrs / 24)
  if (days < 365) return `${days}d`
  return `${Math.floor(days / 365)}y`
}

/** Shorten an EKS ARN context name to just the cluster name for display. */
export function shortContext(name: string): string {
  const m = /cluster\/(.+)$/.exec(name)
  return m ? m[1] : name
}
