import { useEffect, useRef } from 'react'
import type { VantaEffect } from '@/lib/vanta'

// Vanta.js animated background. Effects load lazily (three.js + the chosen effect
// bundle) only once enabled, so the extra weight is paid for only when used.
//
// three is pinned below r152 (see package.json) deliberately: r152+ changed
// WebGLRenderer's default lighting/color-management pipeline, which silently
// breaks vanta 0.5.x's bundled shaders (no error, no thrown exception — the
// scene/camera/renderer all construct fine and render() keeps getting called
// every frame, it just paints nothing perceptible). Confirmed by bisecting:
// pinning three to 0.150.1 restores the effect with zero other code changes.

// Importing a Vanta effect registers its factory on window.VANTA[NAME] (the UMD's
// side effect) — more reliable across bundler interop than the module's default export.
type VantaFactory = (o: Record<string, unknown>) => { destroy: () => void } | null
const LOADERS: Record<VantaEffect, { load: () => Promise<unknown>; global: string }> = {
  net: { load: () => import('vanta/dist/vanta.net.min'), global: 'NET' },
  globe: { load: () => import('vanta/dist/vanta.globe.min'), global: 'GLOBE' },
  waves: { load: () => import('vanta/dist/vanta.waves.min'), global: 'WAVES' },
  rings: { load: () => import('vanta/dist/vanta.rings.min'), global: 'RINGS' },
  halo: { load: () => import('vanta/dist/vanta.halo.min'), global: 'HALO' },
  fog: { load: () => import('vanta/dist/vanta.fog.min'), global: 'FOG' },
}

// Brand-aligned palette (Netsk8 teal, k8s blue, app dark).
const BG = 0x0b0f14
const TEAL = 0x35bec0
const BLUE = 0x326ce5

// Per-effect tuning, layered on top of the shared base options.
const OPTIONS: Record<VantaEffect, Record<string, unknown>> = {
  net: { color: TEAL, backgroundColor: BG, points: 11, maxDistance: 22, spacing: 16, showDots: true },
  globe: { color: TEAL, color2: BLUE, backgroundColor: BG, size: 1.1 },
  waves: { color: BLUE, backgroundColor: BG, shininess: 35, waveHeight: 14, waveSpeed: 0.75, zoom: 0.92 },
  rings: { color: TEAL, backgroundColor: BG },
  halo: { baseColor: 0x11314d, backgroundColor: BG, amplitudeFactor: 1.4, size: 1.4 },
  fog: { baseColor: BG, highlightColor: TEAL, midtoneColor: BLUE, lowlightColor: 0x081019, blurFactor: 0.6, speed: 1.2, zoom: 0.8 },
}

export function VantaBackground({ enabled, effect, opacity }: Readonly<{ enabled: boolean; effect: VantaEffect; opacity: number }>) {
  const ref = useRef<HTMLDivElement>(null)
  const fx = useRef<{ destroy: () => void } | null>(null)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    void (async () => {
      const spec = LOADERS[effect]
      const [, THREE] = await Promise.all([spec.load(), import('three')])
      if (cancelled || !ref.current) return
      const factory = (window as unknown as { VANTA?: Record<string, VantaFactory> }).VANTA?.[spec.global]
      if (!factory) return
      fx.current?.destroy()
      try {
        fx.current =
          factory({
            el: ref.current,
            THREE,
            mouseControls: true,
            touchControls: true,
            gyroControls: false,
            minHeight: 200,
            minWidth: 200,
            scale: 1,
            scaleMobile: 1,
            ...OPTIONS[effect],
          }) ?? null
      } catch (err) {
        // e.g. no WebGL context available — degrade to the static background.
        console.warn('Vanta background unavailable:', err)
        fx.current = null
      }
    })()
    return () => {
      cancelled = true
      fx.current?.destroy()
      fx.current = null
    }
  }, [enabled, effect])

  if (!enabled) return null
  // Sits behind the app (z-0); the translucent panels/cards let it show through.
  // Opacity blends the effect toward the solid app background beneath it.
  return <div ref={ref} className="fixed inset-0" style={{ zIndex: 0, opacity }} aria-hidden />
}
