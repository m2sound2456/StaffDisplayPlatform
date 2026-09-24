import '@testing-library/jest-dom/vitest'

import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// globals are disabled (see vite.config.ts test block), so cleanup is explicit.
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
