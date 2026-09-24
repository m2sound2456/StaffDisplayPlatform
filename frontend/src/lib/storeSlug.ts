/**
 * Store slug rules — shared by the router, the landing page and (from FG5) the
 * store admin forms.
 *
 * The canonical rule lives in docs/BLUEPRINT.md §4 and docs/DATABASE.md: a slug
 * is lowercase, URL safe, 1-63 characters and cannot collide with a platform
 * route such as /app or /setup.
 */

/** Lowercase alphanumeric with single hyphens, 1-63 characters. */
export const STORE_SLUG_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/

/** Paths owned by the platform; a store may never use one of these slugs. */
export const RESERVED_STORE_SLUGS: readonly string[] = [
  'app',
  'setup',
  'api',
  'ws',
  'healthz',
  'readyz',
  'assets',
  'icons',
  'static',
]

export interface StoreSlugValidation {
  valid: boolean
  /** Human readable reason (English, shown to store admins). */
  reason?: string
}

/** Normalises user input into the canonical slug form. */
export function normalizeStoreSlug(value: string | null | undefined): string {
  return (value ?? '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^-+|-+$/g, '')
}

/** Validates a slug against the platform rules. */
export function validateStoreSlug(value: string | null | undefined): StoreSlugValidation {
  const slug = (value ?? '').trim()

  if (slug === '') {
    return { valid: false, reason: 'Store slug is required.' }
  }
  if (slug !== slug.toLowerCase()) {
    return { valid: false, reason: 'Store slug must be lowercase.' }
  }
  if (slug.length > 63) {
    return { valid: false, reason: 'Store slug must be 63 characters or fewer.' }
  }
  if (!STORE_SLUG_PATTERN.test(slug)) {
    return {
      valid: false,
      reason: 'Store slug may only contain lowercase letters, digits and interior hyphens.',
    }
  }
  if (RESERVED_STORE_SLUGS.includes(slug)) {
    return { valid: false, reason: `"${slug}" is reserved by the platform.` }
  }
  return { valid: true }
}

/** Convenience boolean form. */
export function isValidStoreSlug(value: string | null | undefined): boolean {
  return validateStoreSlug(value).valid
}

/** Canonical display route for a store (single domain + path). */
export function storeDisplayPath(slug: string): string {
  return `/s/${slug.trim()}`
}

/** Absolute display URL for QR codes and admin copy-to-clipboard. */
export function storeDisplayUrl(origin: string, slug: string): string {
  return new URL(storeDisplayPath(slug), origin).toString()
}

/** Default slug used by landing page shortcuts. */
export function defaultStoreSlug(): string {
  const configured = (import.meta.env.VITE_DEFAULT_STORE_SLUG ?? '').trim()
  return configured === '' ? 'demo' : configured
}
