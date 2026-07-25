import { setAppPrefs, useAppPrefs } from '@/lib/preferences'

// Vanta animated-background config + settings hook. Non-component exports live
// here (not in the Vanta components) so React Fast Refresh stays happy.

export type VantaEffect = 'net' | 'globe' | 'waves' | 'rings' | 'halo' | 'fog'

export const VANTA_EFFECTS: ReadonlyArray<{ key: VantaEffect; label: string }> = [
  { key: 'net', label: 'Net' },
  { key: 'globe', label: 'Globe' },
  { key: 'waves', label: 'Waves' },
  { key: 'rings', label: 'Rings' },
  { key: 'halo', label: 'Halo' },
  { key: 'fog', label: 'Fog' },
]

const VALID = new Set<VantaEffect>(VANTA_EFFECTS.map((e) => e.key))

export interface VantaSettings {
  enabled: boolean
  effect: VantaEffect
  opacity: number
  setEnabled: (v: boolean) => void
  setEffect: (v: VantaEffect) => void
  setOpacity: (v: number) => void
}

// Vanta background settings, backed by app preferences (localStorage + API mirror).
export function useVantaSettings(): VantaSettings {
  const bg = useAppPrefs().background
  const effect = VALID.has(bg.effect as VantaEffect) ? (bg.effect as VantaEffect) : 'net'
  const set = (patch: Partial<{ enabled: boolean; effect: string; opacity: number }>) =>
    setAppPrefs({ background: { enabled: bg.enabled, effect, opacity: bg.opacity, ...patch } })
  return {
    enabled: bg.enabled,
    effect,
    opacity: bg.opacity,
    setEnabled: (v) => set({ enabled: v }),
    setEffect: (v) => set({ effect: v }),
    setOpacity: (v) => set({ opacity: v }),
  }
}
