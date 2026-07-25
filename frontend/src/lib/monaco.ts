// Wire Monaco to load from the local npm package (no CDN — keeps it offline and
// CSP-safe) and to use a bundled web worker via Vite's ?worker import.
import { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
// monaco-editor's package "exports" remaps "monaco-editor/*" -> "./esm/vs/*",
// so the worker subpath is "editor/editor.worker" (NOT "esm/vs/editor/...").
import editorWorker from 'monaco-editor/editor/editor.worker?worker'

self.MonacoEnvironment = {
  getWorker() {
    // YAML highlighting needs only the base editor worker (no language service).
    return new editorWorker()
  },
}

loader.config({ monaco })
