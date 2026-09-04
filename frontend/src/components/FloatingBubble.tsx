import { useEffect, useRef, useState } from 'react'
import { X } from 'lucide-react'
// Pixabay Content License (free for commercial/noncommercial use, no
// attribution required): https://pixabay.com/illustrations/ai-generated-hot-air-balloon-8115321/
import balloonImg from '@/assets/hot-air-balloon.png'
import { openExternal } from '@/lib/utils'

const BALLOON_WIDTH = 56
const BALLOON_HEIGHT = 69 // source image is 518x640 — keep its aspect ratio
const SPAWN_MARGIN = 120 // how far outside the viewport it starts
const EDGE_MARGIN = 24 // how close to the viewport edge it's allowed to wander, once inside
const MAX_SPEED = 0.035 // px/ms — a gentle drift, not a cross-screen dash
const TURN_RATE = 0.0012 // rad/ms — max random heading change per tick

function rand(min: number, max: number) {
  // Non-cryptographic: only used for cosmetic balloon movement, never for
  // anything security-sensitive. NOSONAR suppresses the generic
  // Math.random()-is-insecure rule (typescript:S2245), which otherwise
  // flags every PRNG use regardless of context.
  return min + Math.random() * (max - min) // NOSONAR
}

// A random point outside the viewport, on a random side — where the balloon
// starts, before it drifts in. Returns the heading (radians) that points
// roughly back toward the viewport, so it drifts inward rather than away.
function spawn() {
  const w = window.innerWidth
  const h = window.innerHeight
  const side = Math.floor(rand(0, 4))
  let pos: { x: number; y: number }
  switch (side) {
    case 0:
      pos = { x: rand(0, w), y: -SPAWN_MARGIN } // above
      break
    case 1:
      pos = { x: w + SPAWN_MARGIN, y: rand(0, h) } // right
      break
    case 2:
      pos = { x: rand(0, w), y: h + SPAWN_MARGIN } // below
      break
    default:
      pos = { x: -SPAWN_MARGIN, y: rand(0, h) } // left
  }
  const heading = Math.atan2(h / 2 - pos.y, w / 2 - pos.x) + rand(-0.4, 0.4)
  return { pos, heading }
}

function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// A hot-air balloon that drifts in from off-screen and wanders the page at
// random, curving gently rather than beelining between fixed points. Generic
// on purpose (message/href) — the demo-mode banner is only its first use; a
// later "update available" notice (unrelated to demo mode) can reuse it.
export function FloatingBubble({ message, href }: Readonly<{ message: string; href: string }>) {
  const [dismissed, setDismissed] = useState(false)
  const elRef = useRef<HTMLDivElement>(null)
  // A snapshot of the media query at mount, not a live subscription — this
  // never changes again after mount, so state (read safely during render,
  // unlike a ref) rather than an active listener is the right amount of
  // reactivity for it.
  const [reducedMotion] = useState(prefersReducedMotion)

  useEffect(() => {
    if (dismissed || reducedMotion) return
    const el = elRef.current
    if (!el) return

    const { pos, heading: initialHeading } = spawn()
    let heading = initialHeading
    const vel = { x: 0, y: 0 }
    let hasEntered = false

    let raf = 0
    let lastT = performance.now()
    const tick = (t: number) => {
      const dt = Math.min(t - lastT, 50) // clamp huge gaps (tab backgrounded, etc.)
      lastT = t

      // Wander: the heading drifts by a small random amount each tick,
      // curving the path smoothly instead of jumping between far waypoints.
      heading += rand(-TURN_RATE, TURN_RATE) * dt

      const desiredVx = Math.cos(heading) * MAX_SPEED
      const desiredVy = Math.sin(heading) * MAX_SPEED
      vel.x += (desiredVx - vel.x) * 0.05
      vel.y += (desiredVy - vel.y) * 0.05

      pos.x += vel.x * dt
      pos.y += vel.y * dt

      if (!hasEntered && pos.x > 0 && pos.x < window.innerWidth && pos.y > 0 && pos.y < window.innerHeight) {
        hasEntered = true
      }
      if (hasEntered) {
        // Bounce gently off the edges (with a little randomness) rather than
        // sliding along the wall.
        if (pos.x < EDGE_MARGIN || pos.x > window.innerWidth - EDGE_MARGIN) {
          heading = Math.PI - heading + rand(-0.3, 0.3)
          pos.x = Math.min(Math.max(pos.x, EDGE_MARGIN), window.innerWidth - EDGE_MARGIN)
        }
        if (pos.y < EDGE_MARGIN || pos.y > window.innerHeight - EDGE_MARGIN) {
          heading = -heading + rand(-0.3, 0.3)
          pos.y = Math.min(Math.max(pos.y, EDGE_MARGIN), window.innerHeight - EDGE_MARGIN)
        }
      }

      el.style.transform = `translate3d(${pos.x - BALLOON_WIDTH / 2}px, ${pos.y - BALLOON_HEIGHT / 2}px, 0)`
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)

    return () => cancelAnimationFrame(raf)
  }, [dismissed, reducedMotion])

  if (dismissed) return null

  return (
    <div
      ref={elRef}
      className="group fixed left-0 top-0 z-50 flex flex-col items-center gap-1"
      style={reducedMotion ? { transform: 'translate3d(calc(100vw - 6rem), calc(100vh - 8rem), 0)' } : undefined}
    >
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="block"
        title={message}
        onClick={(e) => {
          // See openExternal's doc comment — target="_blank" alone does
          // nothing in the desktop app.
          e.preventDefault()
          void openExternal(href)
        }}
      >
        <img src={balloonImg} alt="" width={BALLOON_WIDTH} height={BALLOON_HEIGHT} className="drop-shadow-xl transition-transform group-hover:scale-110" />
      </a>
      <button
        type="button"
        aria-label="Dismiss"
        onClick={() => setDismissed(true)}
        className="rounded-full border bg-popover/95 p-0.5 text-muted-foreground shadow backdrop-blur-xl hover:bg-muted hover:text-foreground"
      >
        <X className="h-3 w-3" />
      </button>
      {/* top-full is relative to this whole group (image + button), so it
          clears the dismiss button instead of overlapping it. */}
      <span className="pointer-events-none absolute left-1/2 top-full mt-1 w-max max-w-48 -translate-x-1/2 rounded-md border bg-popover/95 px-2 py-1 text-center text-[11px] font-medium opacity-0 shadow-lg backdrop-blur-xl transition-opacity group-hover:opacity-100">
        {message}
      </span>
    </div>
  )
}
