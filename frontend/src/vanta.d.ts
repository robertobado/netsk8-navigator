// Vanta ships no types; each effect entry is a factory: (opts) => { destroy(): void }.
declare module 'vanta/dist/*' {
  const effect: (options: Record<string, unknown>) => { destroy: () => void; setOptions?: (o: Record<string, unknown>) => void } | null
  export default effect
}
