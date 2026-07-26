import { parse } from 'yaml'

export interface YamlSyntaxError {
  message: string
  line: number // 1-indexed
  column: number // 1-indexed
}

/** Returns null when `text` is syntactically valid YAML, else its first parse error. */
export function checkYamlSyntax(text: string): YamlSyntaxError | null {
  try {
    parse(text)
    return null
  } catch (e) {
    const err = e as { message?: string; linePos?: Array<{ line: number; col: number }> }
    const pos = err.linePos?.[0]
    return {
      // The library's own message includes a source-context excerpt below the
      // first line — redundant once Monaco underlines the spot itself.
      message: (err.message ?? 'Invalid YAML').split('\n')[0],
      line: pos?.line ?? 1,
      column: pos?.col ?? 1,
    }
  }
}
