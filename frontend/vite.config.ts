import path from 'node:path'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const apiUrl = process.env.TRANSFERLY_API_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    allowedHosts: ['frontend'],
    cors: { origin: [/^https?:\/\/(?:(?:[^:]+\.)?localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/, 'null'] },
    proxy: { '/api': { target: apiUrl, ws: true }, '/healthz': apiUrl, '/readyz': apiUrl },
  },
  build: {
    rollupOptions: {
      input: { main: path.resolve(import.meta.dirname, 'index.html'), viewer: path.resolve(import.meta.dirname, 'sandbox/viewer.html') },
    },
  },
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}', 'guards/**/*.test.ts'],
    // Integration tests with userEvent can exceed 20s when the full suite runs with coverage (many workers).
    testTimeout: process.env.VITEST_COVERAGE === '1' ? 60_000 : 20_000,
    maxWorkers: process.env.VITEST_COVERAGE === '1' ? 4 : undefined,
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/main.tsx',
        'src/sandbox/main.ts',
        'src/components/ui/**',
        '**/*.d.ts',
      ],
      reporter: ['text', 'lcov'],
      thresholds: { statements: 100, branches: 100, functions: 100, lines: 100 },
    },
  },
})
