import { vi } from 'vitest'

import type { HealthReport, VersionInfo } from '@/types/api'

/** Deterministic API fixtures (mirror backend/internal/server/handlers.go). */
export const healthFixture: HealthReport = {
  status: 'ok',
  service: 'staffdisplay-api',
  environment: 'test',
  version: '0.1.0',
  uptime_seconds: 12.5,
  server_time: '2026-01-01T00:00:00Z',
  checks: {
    database: { status: 'ok', latency_ms: 1.5 },
  },
}

export const versionFixture: VersionInfo = {
  version: '0.1.0',
  git_commit: 'abcdef1',
  build_time: '2026-01-01T00:00:00Z',
  go_version: 'go1.26.4',
  environment: 'test',
}

/** JSON Response helper for fetch stubs. */
export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** Success envelope helper. */
export function dataResponse<T>(data: T, status = 200): Response {
  return jsonResponse({ data }, status)
}

/** Failure envelope helper (docs/API.md §1). */
export function errorResponse(status: number, code: string, message: string): Response {
  return jsonResponse({ error: { code, message } }, status)
}

export type FetchHandler = (url: string, init?: RequestInit) => Response | Promise<Response>

/**
 * Replaces global fetch with a deterministic stub.
 * Returns the mock so tests can assert on the request list.
 */
export function stubFetch(handler: FetchHandler) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : (input as Request).url
    return Promise.resolve(handler(url, init))
  })

  vi.stubGlobal('fetch', mock)
  return mock
}

/** Stubs the platform status endpoints used by the FG1 shells. */
export function stubHealthApi(overrides: Partial<{ health: Response; version: Response }> = {}) {
  return stubFetch((url) => {
    if (url.includes('/version')) {
      return overrides.version ?? dataResponse(versionFixture)
    }
    if (url.includes('/health')) {
      return overrides.health ?? dataResponse(healthFixture)
    }
    return errorResponse(404, 'not_found', `no stub for ${url}`)
  })
}
