/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
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
    setupFiles: ['./src/vitest.setup.ts'],
    coverage: { provider: 'v8', reporter: ['text', 'html'] },
  },
})
