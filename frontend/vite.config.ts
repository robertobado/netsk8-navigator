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
  },
  server: {
    port: 5173,
    // Proxy API calls to the Go backend so the browser talks to a single origin.
    // ws:true is required for the exec terminal WebSocket.
    proxy: {
      '/api': { target: 'http://localhost:8080', ws: true, changeOrigin: true },
    },
  },
})
