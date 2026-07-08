import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The Vite build writes into the Go embed directory so `go build` captures the
// compiled SPA into the binary. In dev, API calls are proxied to the Go server.
// The backend target defaults to :8080 but can be overridden with KUKATKO_DEV_API
// (e.g. when :8080 is taken by another service on a shared host).
const apiTarget = process.env.KUKATKO_DEV_API ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
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
