import { useEffect, type RefObject } from 'react'
import type { Monaco } from '@monaco-editor/react'
import type { editor as MonacoEditor } from 'monaco-editor'
import type { YamlSyntaxError } from './yaml'

// Underlines the exact spot of a YAML syntax error in a Monaco editor, the
// same way Monaco's own diagnostics render — shared by ManifestPanel and
// CreateResourceDialog, the two places that edit YAML with live validation.
export function useYamlMarkers(
  editorRef: RefObject<MonacoEditor.IStandaloneCodeEditor | null>,
  monacoRef: RefObject<Monaco | null>,
  yamlError: YamlSyntaxError | null,
) {
  useEffect(() => {
    const ed = editorRef.current
    const monacoNs = monacoRef.current
    const model = ed?.getModel()
    if (!monacoNs || !model) return
    monacoNs.editor.setModelMarkers(
      model,
      'yaml-syntax',
      yamlError
        ? [
            {
              severity: monacoNs.MarkerSeverity.Error,
              message: yamlError.message,
              startLineNumber: yamlError.line,
              startColumn: yamlError.column,
              endLineNumber: yamlError.line,
              endColumn: yamlError.column + 1,
            },
          ]
        : [],
    )
    // editorRef/monacoRef are stable ref objects — only yamlError actually
    // changes across renders, and we always read their latest .current here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [yamlError])
}
