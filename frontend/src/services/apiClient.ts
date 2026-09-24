import { resolveApiBaseUrl } from '@/lib/envProfile'
import type { ApiEnvelope, ApiErrorCode, ApiErrorEnvelope } from '@/types/api'

/**
 * Thin, typed fetch wrapper for the platform API.
 *
 * Responsibilities:
 *  - one place that knows about the base URL and the response envelope
 *  - maps transport failures and API errors onto a single ApiError type
 *  - never sends cookies cross-origin and never logs secrets
 *
 * Components must not call fetch directly (see docs/AI_RULES.md §3).
 */

/** REST base URL. `/api/v1` on the platform origin unless a profile overrides it. */
export const API_BASE_URL = resolveApiBaseUrl(import.meta.env.VITE_API_BASE_URL, import.meta.env.MODE)

export type QueryValue = string | number | boolean | null | undefined

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  /** JSON request body (serialised automatically). */
  body?: unknown
  /** Query string parameters; null/undefined entries are skipped. */
  query?: Record<string, QueryValue>
  headers?: Record<string, string>
  signal?: AbortSignal
}

/** Transport independent API error. */
export class ApiError extends Error {
  readonly status: number
  readonly code: ApiErrorCode | string
  readonly details?: unknown

  constructor(status: number, code: ApiErrorCode | string, message: string, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }

  /** True when the request never reached the server. */
  get isNetworkError(): boolean {
    return this.status === 0
  }
}

/** Builds the absolute request URL for a path plus optional query values. */
export function buildApiUrl(path: string, query?: Record<string, QueryValue>): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const url = `${API_BASE_URL}${normalizedPath}`

  if (!query) {
    return url
  }

  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') {
      continue
    }
    search.append(key, String(value))
  }

  const queryString = search.toString()
  return queryString === '' ? url : `${url}?${queryString}`
}

function hasDataEnvelope(value: unknown): value is ApiEnvelope<unknown> {
  return typeof value === 'object' && value !== null && 'data' in value
}

function isErrorEnvelope(value: unknown): value is ApiErrorEnvelope {
  if (typeof value !== 'object' || value === null || !('error' in value)) {
    return false
  }
  const { error } = value as { error?: unknown }
  return typeof error === 'object' && error !== null && 'code' in error
}

function parseJson(text: string): unknown {
  if (text.trim() === '') {
    return undefined
  }
  try {
    return JSON.parse(text) as unknown
  } catch {
    return undefined
  }
}

/** Performs a request and unwraps the `data` envelope. */
export async function apiFetch<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', ...options.headers }
  let body: string | undefined

  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(options.body)
  }

  let response: Response
  try {
    response = await fetch(buildApiUrl(path, options.query), {
      method: options.method ?? 'GET',
      headers,
      body,
      signal: options.signal,
      credentials: 'same-origin',
      cache: 'no-store',
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new ApiError(0, 'network_error', 'Unable to reach the Staff Display API', error)
  }

  const payload = parseJson(await response.text())

  if (!response.ok) {
    if (isErrorEnvelope(payload)) {
      throw new ApiError(response.status, payload.error.code, payload.error.message, payload.error.details)
    }
    throw new ApiError(response.status, 'unknown_error', `Request failed with status ${response.status}`)
  }

  if (hasDataEnvelope(payload)) {
    return payload.data as T
  }
  return payload as T
}

/** Convenience HTTP helpers. */
export const api = {
  get: <T>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>): Promise<T> =>
    apiFetch<T>(path, { ...options, method: 'GET' }),
  post: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>): Promise<T> =>
    apiFetch<T>(path, { ...options, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>): Promise<T> =>
    apiFetch<T>(path, { ...options, method: 'PUT', body }),
  patch: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>): Promise<T> =>
    apiFetch<T>(path, { ...options, method: 'PATCH', body }),
  delete: <T>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>): Promise<T> =>
    apiFetch<T>(path, { ...options, method: 'DELETE' }),
}
