import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'

// Vitest doesn't expose global `afterEach` unless `test.globals` is enabled,
// so Testing Library's own auto-cleanup detection (which looks for exactly
// that global) never fires — do it explicitly instead, or DOM from one test
// leaks into the next and text queries start matching multiple elements.
afterEach(() => {
  cleanup()
})
