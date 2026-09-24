/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

interface ImportMetaEnv {
  /** REST base URL, e.g. "/api/v1" (same origin in production). */
  readonly VITE_API_BASE_URL?: string
  /** Default store slug used by landing page shortcuts. */
  readonly VITE_DEFAULT_STORE_SLUG?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
