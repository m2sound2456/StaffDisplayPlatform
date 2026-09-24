/**
 * PWA service worker registration.
 *
 * `vite-plugin-pwa` generates the Workbox service worker at build time
 * (see vite.config.ts). It is registered here rather than injected so the
 * behaviour is explicit, testable and easy to extend in FG27–FG30.
 *
 * Notes:
 *  - development builds skip registration (devOptions.enabled = false)
 *  - the API is never cached by the SW: the display layer owns its offline cache
 */
export function registerServiceWorker(): void {
  if (!import.meta.env.PROD) {
    return
  }

  void import('virtual:pwa-register')
    .then(({ registerSW }) => {
      registerSW({
        immediate: true,
        onOfflineReady() {
          console.info('[pwa] offline shell ready')
        },
        onRegisterError(error) {
          console.warn('[pwa] service worker registration failed', error)
        },
      })
    })
    .catch((error: unknown) => {
      console.warn('[pwa] service worker unavailable', error)
    })
}
