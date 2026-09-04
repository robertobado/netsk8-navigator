/// <reference types="vitest/config" />
import { defineConfig } from 'vite'

// Separate from vitest.config.ts on purpose: *.contract.test.ts files spawn
// the real Go backend (see testsupport/gateServerGlobalSetup.ts) and need
// the Go toolchain on PATH. Keeping them out of the default `pnpm test`
// include pattern means the normal unit-test run (and CI jobs without Go)
// never depends on that. Run these with `pnpm test:contract`.
export default defineConfig({
  test: {
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    setupFiles: ['./src/vitest.setup.ts'],
    include: ['src/**/*.contract.test.{ts,tsx}'],
    globalSetup: ['./testsupport/gateServerGlobalSetup.ts'],
    // These hit a real process + real HTTP; give them more room than the
    // default 5s unit-test timeout, especially on a cold `go run` build.
    testTimeout: 20_000,
    hookTimeout: 30_000,
  },
})
