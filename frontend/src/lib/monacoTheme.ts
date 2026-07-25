import type { Monaco } from '@monaco-editor/react'

// A Monaco theme derived from the app's own palette so the YAML editor blends
// into the UI instead of looking like a stock vs-dark box. Colors are read from
// the live CSS variables and normalized to hex — robustly, since a computed
// color may come back as rgb(), oklch(), color(), etc. depending on the browser.

export const NETSK8_THEME = 'netsk8-dark'
let defined = false

// Normalize any CSS color the browser understands to #rrggbb by *painting* it on
// a 1×1 canvas and reading the pixel back: the browser rasterizes to real sRGB
// bytes regardless of the input color space (rgb, oklch, color(), …). Parsing the
// serialized string is unreliable — oklch channels get misread as rgb.
function toHex(color: string): string {
  try {
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const ctx = canvas.getContext('2d', { willReadFrequently: true })
    if (!ctx) return '#808080'
    ctx.fillStyle = '#808080' // stays if `color` is rejected (e.g. unsupported space)
    ctx.fillStyle = color
    ctx.fillRect(0, 0, 1, 1)
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data
    const h = (n: number) => n.toString(16).padStart(2, '0')
    return `#${h(r)}${h(g)}${h(b)}`
  } catch {
    return '#808080'
  }
}

// Resolve CSS custom properties to concrete hex via a throwaway probe element.
function resolve(names: string[]): Record<string, string> {
  const probe = document.createElement('span')
  probe.style.position = 'absolute'
  probe.style.visibility = 'hidden'
  document.body.appendChild(probe)
  const out: Record<string, string> = {}
  for (const n of names) {
    probe.style.color = `var(${n})`
    out[n] = toHex(getComputedStyle(probe).color)
  }
  probe.remove()
  return out
}

// Registers the theme once and returns its name. Always defines NETSK8_THEME
// (falling back to a bare vs-dark clone on any error) so the editor's `theme`
// prop is never a dangling reference — a theme error must never blank the UI.
export function ensureNetsk8Theme(monaco: Monaco): string {
  if (defined) return NETSK8_THEME
  try {
    const c = resolve(['--card', '--popover', '--foreground', '--muted-foreground', '--border', '--brand', '--primary', '--ok', '--warn', '--accent'])
    const bare = (hex: string) => hex.slice(1) // token rules want hex without '#'
    monaco.editor.defineTheme(NETSK8_THEME, {
      base: 'vs-dark',
      inherit: true,
      rules: [
        { token: '', foreground: bare(c['--foreground']) },
        { token: 'comment', foreground: bare(c['--muted-foreground']), fontStyle: 'italic' },
        { token: 'type', foreground: bare(c['--brand']) }, // YAML keys
        { token: 'string', foreground: bare(c['--ok']) },
        { token: 'number', foreground: bare(c['--primary']) },
        { token: 'keyword', foreground: bare(c['--warn']) }, // true/false/null
        { token: 'tag', foreground: bare(c['--brand']) },
      ],
      colors: {
        'editor.background': c['--card'],
        'editor.foreground': c['--foreground'],
        'editorLineNumber.foreground': c['--muted-foreground'] + '66',
        'editorLineNumber.activeForeground': c['--foreground'],
        'editor.lineHighlightBackground': c['--accent'] + '55',
        'editor.lineHighlightBorder': '#00000000',
        'editorCursor.foreground': c['--brand'],
        'editor.selectionBackground': c['--primary'] + '3a',
        'editor.inactiveSelectionBackground': c['--primary'] + '22',
        'editorIndentGuide.background1': c['--border'],
        'editorIndentGuide.activeBackground1': c['--brand'] + '99',
        'editorGutter.background': c['--card'],
        'editorWidget.background': c['--popover'],
        'editorWidget.border': c['--border'],
        'editorBracketMatch.background': c['--brand'] + '22',
        'editorBracketMatch.border': c['--brand'] + '66',
        'scrollbarSlider.background': c['--muted-foreground'] + '2e',
        'scrollbarSlider.hoverBackground': c['--muted-foreground'] + '55',
        'scrollbarSlider.activeBackground': c['--muted-foreground'] + '77',
        'editorOverviewRuler.border': '#00000000',
      },
    })
  } catch {
    // Never let a theme problem crash the editor — fall back to a plain clone.
    try {
      monaco.editor.defineTheme(NETSK8_THEME, { base: 'vs-dark', inherit: true, rules: [], colors: {} })
    } catch {
      /* defineTheme itself unavailable — the prop will just resolve to vs-dark */
    }
  }
  defined = true
  return NETSK8_THEME
}
