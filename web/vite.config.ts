import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

import { gitkeepPlugin } from './build/gitkeep'
import { localFontsPlugin } from './build/localFonts'
import { pwaPlugin } from './build/pwa'

// The Vite build writes into the Go embed directory so `go build` captures the
// compiled SPA into the binary. In dev, API calls are proxied to the Go server.
// The backend target defaults to :8080 but can be overridden with KUKATKO_DEV_API
// (e.g. when :8080 is taken by another service on a shared host).
const apiTarget = process.env.KUKATKO_DEV_API ?? 'http://localhost:8080'

export default defineConfig({
  // localFontsPlugin runs in dev too: it strips Bootswatch's remote webfont
  // `@import` before Vite's CSS pipeline hoists it into the bundle, and fails
  // the build if any asset still names a font host (see build/localFonts.ts).
  // The other two only apply to `vite build`: pwaPlugin emits /sw.js with the
  // precache manifest of the finished bundle (see build/pwa.ts), and
  // gitkeepPlugin puts the tracked dist/.gitkeep back after emptyOutDir wiped
  // it — a deleted tracked file is what stamped release binaries +dirty (see
  // build/gitkeep.ts).
  plugins: [localFontsPlugin(), react(), pwaPlugin(), gitkeepPlugin()],
  build: {
    outDir: '../internal/web/static/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/healthz': apiTarget,
      '/api': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    restoreMocks: true,
  },
})
