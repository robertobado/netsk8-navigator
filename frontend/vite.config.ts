/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import { configDefaults } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  build: {
    // The Monaco editor is inherently large (~2.6 MB) and is already code-split
    // to load only when a YAML tab opens (see ManifestPanelLazy), so the 500 kB
    // default warning is unrealistic here. Raise it just above Monaco's chunk;
    // anything genuinely larger still warns.
    chunkSizeWarningLimit: 3000,
    // Build straight into the Go module so `go build` can //go:embed it,
    // producing a single binary that serves both the API and the UI
    // (see internal/web). Dev mode is unaffected — it still uses the Vite
    // dev server below, proxying /api to the Go backend.
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    // Listen on all interfaces (not just localhost) so devices on the same
    // LAN (e.g. a phone, for mobile responsiveness testing) can reach it.
    host: true,
    // Proxy API calls to the Go backend so the browser talks to a single origin.
    // ws:true is required for the exec terminal WebSocket.
    proxy: {
      '/api': { target: 'http://localhost:8080', ws: true, changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    // Without an explicit http(s) URL, jsdom treats the document as an opaque
    // origin and leaves `localStorage` undefined — several lib modules
    // (usage.ts, preferences.ts) touch it directly at the top level.
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    setupFiles: ['./src/vitest.setup.ts'],
    // *.contract.test.ts spawns the real Go backend (see
    // vitest.contract.config.ts) and needs the Go toolchain on PATH — kept
    // out of the default run so `pnpm test` never depends on that. Run
    // those with `pnpm test:contract`.
    exclude: [...configDefaults.exclude, '**/*.contract.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      // 'lcov' is what SonarCloud's sonar.javascript.lcov.reportPaths expects
      // (see ../sonar-project.properties + the sonarcloud job in ci.yml).
      reporter: ['text', 'html', 'lcov'],
      // Vitest 4 only instruments files actually imported by a test unless
      // told otherwise — without this, dozens of untested components would
      // simply be absent from the report instead of showing up as 0%.
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/vitest.setup.ts', 'src/vite-env.d.ts', 'src/main.tsx'],
    },
  },
})
