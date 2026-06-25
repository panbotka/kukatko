import '@testing-library/jest-dom/vitest'

import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// React Testing Library does not auto-clean between tests under Vitest's
// default config, so unmount rendered trees after each test to avoid leakage.
afterEach(() => {
  cleanup()
})
