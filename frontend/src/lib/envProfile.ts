/**
 * Environment profile rules for the Vite build (FG3).
 *
 * One SPA build is served from the platform origin by nginx, so every profile
 * except development must use a same-origin API base path: an absolute URL
 * would require CORS, break the single-domain architecture and tie the bundle to
 * a host that is never compiled in (BLUEPRINT §32, decision D1).
 *
 * The rules are shared by the build (`vite.config.ts` refuses to build a bad
 * profile) and by the runtime client (`src/services/apiClient.ts`).
 */

/** Fallback REST base when a profile does not configure one. */
export const DEFAULT_API_BASE_URL = '/api/v1'

/** Modes that must stay same-origin (the platform host serves both bundles). */
const SAME_ORIGIN_MODES = ['staging', 'production']

/** Local hosts that development may point at directly. */
const LOCAL_HOSTNAMES = ['127.0.0.1', 'localhost', '::1', '[::1]']

/**
 * Resolves the REST base URL of a profile and throws when the value would break
 * the single-domain architecture.
 */
export function resolveApiBaseUrl(raw: string | undefined, mode: string): string {
  const trimmed = (raw ?? '').trim()
  if (trimmed === '') {
    return DEFAULT_API_BASE_URL
  }

  const base = trimmed.replace(/\/+$/, '')
  if (base === '' || base === '/') {
    return DEFAULT_API_BASE_URL
  }
  if (base.startsWith('//')) {
    throw new Error(`VITE_API_BASE_URL must not be protocol relative (got "${trimmed}")`)
  }
  if (/\s/.test(base) || base.includes('?') || base.includes('#')) {
    throw new Error(`VITE_API_BASE_URL must be a plain path without whitespace, query or fragment (got "${trimmed}")`)
  }
  if (base.startsWith('/')) {
    return base
  }
  if (SAME_ORIGIN_MODES.includes(mode) || !isLocalhostUrl(base)) {
    throw new Error(
      `VITE_API_BASE_URL must be a same-origin path such as "${DEFAULT_API_BASE_URL}" in the "${mode}" profile ` +
        `(got "${trimmed}"). Only local development may point at another host.`,
    )
  }
  return base
}

/** True when the value is an absolute HTTP(S) URL on a local host. */
function isLocalhostUrl(value: string): boolean {
  let url: URL
  try {
    url = new URL(value)
  } catch {
    return false
  }
  return (url.protocol === 'http:' || url.protocol === 'https:') && LOCAL_HOSTNAMES.includes(url.hostname)
}
