import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The Vite build writes into the Go embed directory so `go build` captures the
// compiled SPA into the binary. In dev, API calls are proxied to the Go server.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/web/static/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/healthz': 'http://localhost:8080',
      '/api': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    restoreMocks: true,
  },
})
