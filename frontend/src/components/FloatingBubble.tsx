import { useEffect, useState } from 'react'
import { ExternalLink, X, type LucideIcon } from 'lucide-react'

const CORNERS = [
  { top: '5rem', left: '1.5rem' },
  { top: '5rem', right: '1.5rem' },
  { bottom: '2rem', right: '1.5rem' },
  { bottom: '2rem', left: '1.5rem' },
] as const

const ROAM_INTERVAL_MS = 15000

// A small pill that roams between the four screen corners, nudging attention
// toward `href` without blocking any fixed content. Generic on purpose — the
// demo-mode banner is only its first use; a later "update available" notice
// (unrelated to demo mode) reuses the same component with different props.
export function FloatingBubble({ message, href, icon: Icon = ExternalLink }: Readonly<{ message: string; href: string; icon?: LucideIcon }>) {
  const [cornerIndex, setCornerIndex] = useState(0)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    const id = setInterval(() => setCornerIndex((i) => (i + 1) % CORNERS.length), ROAM_INTERVAL_MS)
    return () => clearInterval(id)
  }, [])

  if (dismissed) return null

  return (
    <div
      style={CORNERS[cornerIndex]}
      className="fixed z-50 flex items-center gap-2 rounded-full border bg-popover/95 py-2 pl-4 pr-2 text-xs shadow-2xl shadow-black/40 backdrop-blur-xl transition-all duration-1000 ease-in-out"
    >
      <a href={href} target="_blank" rel="noreferrer" className="flex items-center gap-2 font-medium hover:underline">
        <Icon className="h-4 w-4" />
        {message}
      </a>
      <button type="button" aria-label="Dismiss" onClick={() => setDismissed(true)} className="rounded-full p-1 text-muted-foreground hover:bg-muted">
        <X className="h-3 w-3" />
      </button>
    </div>
  )
}
