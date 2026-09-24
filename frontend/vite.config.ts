import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { loadEnv } from 'vite'
import { defineConfig } from 'vitest/config'
import { VitePWA } from 'vite-plugin-pwa'

import { resolveApiBaseUrl } from './src/lib/envProfile'

/**
 * The SPA is served from the platform root by nginx and only talks to the API on
 * the same origin (single domain + path, BLUEPRINT §32):
 *
 *   /                landing
 *   /app             admin application
 *   /s/{store-slug}  store display (tablet)
 *   /setup           device setup + QR pairing
 *   /api/v1          Go API (proxied in development)
 */
const API_TARGET = process.env.VITE_DEV_API_TARGET ?? 'http://127.0.0.1:8080'

export default defineConfig(({ mode }) => {
  // Environment profiles (FG3): .env.development / .env.staging /
  // .env.production. An API base URL that would break the single-domain rule
  // fails the build instead of shipping a CORS dependent bundle.
  const env = loadEnv(mode, process.cwd(), 'VITE_')
  resolveApiBaseUrl(env.VITE_API_BASE_URL, mode)

  return {
    plugins: [
      react(),
      tailwindcss(),
      VitePWA({
        registerType: 'autoUpdate',
        // The service worker is registered from src/pwa/registerServiceWorker.ts.
        injectRegister: null,
        includeAssets: ['favicon.svg', 'icons/apple-touch-icon.png'],
        manifest: {
          name: 'Staff Display Platform',
          short_name: 'StaffDisplay',
          description: 'Multi-tenant staff display for store tablets (stand and handheld mode)',
          lang: 'en',
          start_url: '/',
          scope: '/',
          display: 'fullscreen',
          orientation: 'any',
          background_color: '#0f172a',
          theme_color: '#0f172a',
          icons: [
            { src: 'icons/icon-192.png', sizes: '192x192', type: 'image/png' },
            { src: 'icons/icon-512.png', sizes: '512x512', type: 'image/png' },
            { src: 'icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
          ],
        },
        workbox: {
          globPatterns: ['**/*.{js,css,html,svg,png,ico,webmanifest}'],
          navigateFallback: '/index.html',
          cleanupOutdatedCaches: true,
          // API and probe traffic must never be answered from the SW cache: the
          // display layer owns its own offline cache (FG28).
          navigateFallbackDenylist: [/^\/api\//, /^\/ws/, /^\/healthz/, /^\/readyz/],
        },
        devOptions: { enabled: false },
      }),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      host: '127.0.0.1',
      port: 5173,
      strictPort: true,
      proxy: {
        '/api': { target: API_TARGET, changeOrigin: true },
        '/healthz': { target: API_TARGET, changeOrigin: true },
        '/readyz': { target: API_TARGET, changeOrigin: true },
      },
    },
    preview: {
      host: '127.0.0.1',
      port: 4173,
    },
    build: {
      outDir: 'dist',
      sourcemap: false,
      target: 'es2022',
    },
    test: {
      environment: 'jsdom',
      setupFiles: ['./vitest.setup.ts'],
      include: ['src/**/*.test.{ts,tsx}'],
      restoreMocks: true,
      css: false,
    },
  }
})
