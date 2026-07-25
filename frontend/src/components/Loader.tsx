/**
 * NavigatorLoader — homage to the classic Netscape Navigator throbber, in pure
 * SVG + CSS: a serif "N" standing on a curved planet horizon under a starfield,
 * with glowing meteors streaking diagonally across the sky and passing behind
 * the N. The meteor heads are little hexagons — a subtle Kubernetes-pod nod.
 * Framed in the brand's metallic porthole.
 */
import { useT } from '@/lib/i18n'

// Deterministic starfield (x, y, r, color, twinkle duration, delay).
const STARS: [number, number, number, string, number, number][] = [
  [22, 20, 0.9, '#fff', 3.1, 0],
  [34, 12, 0.7, '#bfe9ff', 2.4, 0.6],
  [48, 24, 0.8, '#fff', 3.6, 1.2],
  [70, 14, 0.7, '#ffe9a8', 2.8, 0.3],
  [86, 22, 0.9, '#fff', 3.2, 0.9],
  [96, 34, 0.7, '#bfe9ff', 2.5, 1.5],
  [16, 38, 0.8, '#fff', 3.4, 0.4],
  [30, 46, 0.6, '#c9fff0', 2.7, 1.1],
  [58, 16, 0.6, '#fff', 3.0, 1.8],
  [78, 40, 0.8, '#fff', 2.9, 0.2],
  [92, 50, 0.7, '#ffe9a8', 3.5, 1.3],
  [12, 54, 0.7, '#bfe9ff', 3.1, 0.7],
  [40, 34, 0.6, '#fff', 2.6, 1.6],
  [64, 30, 0.7, '#fff', 3.3, 0.5],
  [104, 44, 0.6, '#c9fff0', 2.8, 1.0],
  [26, 62, 0.6, '#fff', 3.0, 1.4],
  [50, 56, 0.5, '#fff', 2.5, 0.8],
  [82, 60, 0.6, '#bfe9ff', 3.4, 0.1],
]

// Comets follow curved paths (arcs echoing the planet's curvature), right -> left.
// Four sweep BEHIND the N across the sky; the fifth sweeps in FRONT, low over the
// blue ground and the helm.
interface Comet {
  d: string
  dur: number
  delay: number
  color: string
  trail: number
}
const BEHIND: Comet[] = [
  { d: 'M108 22 Q60 12 12 26', dur: 3.0, delay: 0.0, color: 'white', trail: 30 },
  { d: 'M108 33 Q60 23 12 37', dur: 3.0, delay: 0.3, color: 'cyan', trail: 28 },
  { d: 'M108 44 Q60 34 12 48', dur: 3.0, delay: 0.6, color: 'white', trail: 30 },
  { d: 'M108 54 Q60 46 12 57', dur: 3.0, delay: 0.9, color: 'cyan', trail: 26 },
]
// Enters right-middle, sweeps down in one smooth (monotonic) curve over the blue
// ground and the helm, exits lower-left. Control point sits BETWEEN the endpoints'
// heights so the arc never rises again (no zigzag).
const FRONT: Comet = { d: 'M108 54 Q66 90 18 96', dur: 3.0, delay: 1.5, color: 'cyan', trail: 44 }

export function NavigatorLoader({
  size = 140,
  state = 'connecting',
  label,
  sky = 'navy',
}: {
  size?: number
  state?: 'connecting' | 'ready'
  label?: string
  sky?: 'navy' | 'green'
}) {
  const t = useT()
  const cls = ['nk', state === 'ready' && 'nk--ready'].filter(Boolean).join(' ')
  const wheel = wheelVerts(60, 89, 14) // Kubernetes helm below the horizon (handle tips)

  return (
    <div className={cls} style={{ width: size }}>
      <style>{CSS}</style>
      <svg viewBox="0 0 120 120" width={size} height={size} role="img" aria-label={t('Netsk8 Navigator loading')}>
        <defs>
          <clipPath id="nk-disc">
            <circle cx="60" cy="60" r="48" />
          </clipPath>
          {sky === 'green' ? (
            // Netscape teal sky: darker teal at top, brightening to cyan at the horizon.
            <linearGradient id="nk-space-green" gradientUnits="userSpaceOnUse" x1="0" y1="6" x2="0" y2="74">
              <stop offset="0" stopColor="#1c7f88" />
              <stop offset="0.55" stopColor="#309ea3" />
              <stop offset="1" stopColor="#74cdcd" />
            </linearGradient>
          ) : (
            <radialGradient id="nk-space-navy" cx="0.5" cy="0.35" r="0.85">
              <stop offset="0" stopColor="#0d2137" />
              <stop offset="0.6" stopColor="#081627" />
              <stop offset="1" stopColor="#03070f" />
            </radialGradient>
          )}
          <radialGradient id="nk-atmo" cx="0.5" cy="1" r="0.75">
            <stop offset="0" stopColor="#5a8cff" stopOpacity="0.6" />
            <stop offset="0.5" stopColor="#326ce5" stopOpacity="0.2" />
            <stop offset="1" stopColor="#326ce5" stopOpacity="0" />
          </radialGradient>
          {/* Kubernetes blue (#326ce5) ground */}
          <linearGradient id="nk-ground" x1="0" y1="70" x2="0" y2="112" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#4d84f5" />
            <stop offset="0.4" stopColor="#2a5fd0" />
            <stop offset="1" stopColor="#0b2450" />
          </linearGradient>
          <linearGradient id="nk-steel" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#eef4f7" />
            <stop offset="0.45" stopColor="#9fb0bd" />
            <stop offset="0.55" stopColor="#5f7180" />
            <stop offset="1" stopColor="#cdd8e0" />
          </linearGradient>
          {['cyan', 'white', 'violet'].map((c) => (
            <linearGradient key={c} id={`nk-trail-${c}`} x1="1" y1="0" x2="0" y2="0">
              <stop offset="0" stopColor={TRAIL[c].head} stopOpacity="0.95" />
              <stop offset="1" stopColor={TRAIL[c].tail} stopOpacity="0" />
            </linearGradient>
          ))}
          <filter id="nk-glow" x="-120%" y="-120%" width="340%" height="340%">
            <feGaussianBlur stdDeviation="1.6" result="b" />
            <feMerge>
              <feMergeNode in="b" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>

        <g clipPath="url(#nk-disc)">
          {/* Deep space */}
          <rect x="0" y="0" width="120" height="120" fill={`url(#nk-space-${sky})`} />

          {/* Starfield */}
          {STARS.map(([x, y, r, fill, tw, d], i) => (
            <circle key={i} cx={x} cy={y} r={r} fill={fill} className="nk-star" style={{ ['--tw' as string]: `${tw}s`, ['--d' as string]: `${d}s` }} />
          ))}

          {/* Four comets sweeping BEHIND the N (curved arcs across the sky) */}
          <g filter="url(#nk-glow)">
            {BEHIND.map((c) => (
              <CometTail key={c.d} comet={c} dots={7} headR={2.7} />
            ))}
          </g>

          {/* N — right leg + diagonal FIRST, so the ground occludes their base (they sit BEHIND the horizon) */}
          <path d="M76 28 V78 M44 28 L76 74" stroke="#eef4f7" strokeWidth="8" strokeLinecap="round" strokeLinejoin="round" fill="none" />

          {/* Planet ground — low, gently curved (big radius), teal gradient filling the whole lower disc */}
          <circle cx="60" cy="210" r="140" fill="url(#nk-ground)" />
          <ellipse cx="60" cy="64" rx="58" ry="18" fill="url(#nk-atmo)" />
          <circle cx="60" cy="210" r="140" fill="none" stroke="#9cc0ff" strokeWidth="1.3" opacity="0.75" />

          {/* Kubernetes helm — larger 7-spoke ship's wheel below the horizon */}
          <g stroke="#eef4fb" fill="none" opacity="0.94" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="60" cy="89" r="11.5" strokeWidth="1.7" />
            <circle cx="60" cy="89" r="5.8" strokeWidth="1.3" />
            {wheel.map((p) => (
              <line key={`s${p[0]}-${p[1]}`} x1="60" y1="89" x2={p[0]} y2={p[1]} strokeWidth="1.6" />
            ))}
            {wheel.map((p) => (
              <circle key={`h${p[0]}-${p[1]}`} cx={p[0]} cy={p[1]} r="1.8" fill="#eef4fb" stroke="none" />
            ))}
            <circle cx="60" cy="89" r="2.6" fill="#eef4fb" stroke="none" />
          </g>

          {/* N — left leg LAST, IN FRONT of the horizon (its foot rests on the ground) */}
          <path d="M44 78 V28" stroke="#f4fafc" strokeWidth="8" strokeLinecap="round" fill="none" />

          {/* Fifth comet — sweeps in FRONT, low over the blue ground and the helm */}
          <g filter="url(#nk-glow)">
            <CometTail comet={FRONT} dots={11} headR={3.8} />
          </g>
        </g>

        {/* Metallic porthole bezel */}
        <circle cx="60" cy="60" r="52.5" fill="none" stroke="url(#nk-steel)" strokeWidth="5.5" />
        <circle cx="60" cy="60" r="48.6" fill="none" stroke="#0a1622" strokeWidth="1.4" opacity="0.85" />
        <circle cx="60" cy="60" r="55.4" fill="none" stroke="#0a1622" strokeWidth="1.2" opacity="0.5" />
      </svg>

      {label && <p className="nk-label">{label}</p>}
    </div>
  )
}

/** Standalone preview screen (open at #loader) to review the loader in isolation. */
export function LoaderPreview() {
  return (
    <div className="relative z-10 flex min-h-screen flex-col items-center justify-center gap-16 p-10">
      <div className="flex flex-wrap items-start justify-center gap-16">
        <div className="flex flex-col items-center gap-4">
          <NavigatorLoader size={220} state="connecting" label="Connecting…" sky="navy" />
          <span className="text-xs text-muted-foreground">sky="navy" (atual)</span>
        </div>
        <div className="flex flex-col items-center gap-4">
          <NavigatorLoader size={220} state="connecting" label="Connecting…" sky="green" />
          <span className="text-xs text-muted-foreground">sky="green" (Netscape)</span>
        </div>
      </div>
      <div className="flex items-end gap-8">
        <NavigatorLoader size={40} sky="green" />
        <NavigatorLoader size={72} sky="green" />
        <NavigatorLoader size={110} sky="green" />
      </div>
      <p className="text-sm text-muted-foreground">Verde nos tamanhos 40 / 72 / 110 — SVG puro, escala sem perda.</p>
    </div>
  )
}

const TRAIL: Record<string, { head: string; tail: string }> = {
  cyan: { head: '#dbeaff', tail: '#3f7cf0' },
  white: { head: '#ffffff', tail: '#9db8e8' },
  violet: { head: '#efe9ff', tail: '#8f7fff' },
}

// A comet drawn as a string of dots along its offset-path, so the tail hugs the
// curve (a rigid straight trail would "whip" on a curved path and read as zigzag).
// Dot 0 is the bright hexagonal head; the rest are fading round tail dots.
function CometTail({ comet, dots, headR }: { comet: Readonly<Comet>; dots: number; headR: number }) {
  return (
    <>
      {Array.from({ length: dots }, (_, i) => {
        const t = dots > 1 ? i / (dots - 1) : 0
        const r = +(headR * (1 - t * 0.7)).toFixed(2)
        const style = {
          offsetPath: `path("${comet.d}")`,
          animationDuration: `${comet.dur}s`,
          animationDelay: `${(comet.delay + i * 0.03).toFixed(3)}s`,
          ['--max' as string]: (1 - t * 0.9).toFixed(2),
        }
        return i === 0 ? (
          <polygon key={comet.d + i} className="nk-fly" style={style} points={hexPoints(r)} fill={TRAIL[comet.color].head} />
        ) : (
          <circle key={comet.d + i} className="nk-fly" style={style} r={r} fill={TRAIL[comet.color].tail} />
        )
      })}
    </>
  )
}

// 7 evenly-spaced vertices of the Kubernetes wheel, starting at the top.
function wheelVerts(cx: number, cy: number, r: number): [number, number][] {
  return Array.from({ length: 7 }, (_, i) => {
    const a = ((-90 + (i * 360) / 7) * Math.PI) / 180
    return [+(cx + Math.cos(a) * r).toFixed(2), +(cy + Math.sin(a) * r).toFixed(2)] as [number, number]
  })
}

// Pointy-top hexagon centered at (0,0).
function hexPoints(r: number): string {
  return [-90, -30, 30, 90, 150, 210]
    .map((deg) => {
      const a = (deg * Math.PI) / 180
      return `${(Math.cos(a) * r).toFixed(2)},${(Math.sin(a) * r).toFixed(2)}`
    })
    .join(' ')
}

const CSS = `
.nk { display: flex; flex-direction: column; align-items: center; gap: 0.75rem; }
.nk svg { display: block; }
.nk-label { font-size: 0.8rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--muted-foreground); margin: 0; }

.nk-star { animation: nk-twinkle var(--tw, 3s) ease-in-out infinite; animation-delay: var(--d, 0s); }
@keyframes nk-twinkle { 0%, 100% { opacity: 0.25; } 50% { opacity: 1; } }

/* Comets follow their curved offset-path, then stay hidden until the next pass. */
.nk-fly {
  offset-rotate: auto;
  animation-name: nk-fly;
  animation-timing-function: cubic-bezier(0.35, 0, 0.65, 1);
  animation-iteration-count: infinite;
  will-change: offset-distance, opacity;
}
@keyframes nk-fly {
  0%   { offset-distance: 0%; opacity: 0; }
  6%   { opacity: 0; }
  14%  { opacity: var(--max, 1); }
  44%  { opacity: var(--max, 1); }
  54%  { offset-distance: 100%; opacity: 0; }
  100% { offset-distance: 100%; opacity: 0; }
}
/* Ready: comets ease off. */
.nk--ready .nk-fly { animation-duration: 5s; }

@media (prefers-reduced-motion: reduce) {
  .nk-fly { animation-duration: 6s; }
  .nk-star { animation: none; opacity: 0.8; }
}
`
