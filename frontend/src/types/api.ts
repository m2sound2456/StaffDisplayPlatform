/**
 * API types shared by the Admin, Display and Setup screens.
 *
 * The REST contract lives in docs/API.md. Keep these types in sync with the Go
 * handlers in backend/internal/server.
 */

/** Success envelope: { "data": ... } */
export interface ApiEnvelope<T> {
  data: T
}

/** Failure envelope: { "error": { code, message, details? } } */
export interface ApiErrorEnvelope {
  error: {
    code: string
    message: string
    details?: unknown
  }
}

/** Machine readable error codes (docs/API.md §1). */
export type ApiErrorCode =
  | 'bad_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'method_not_allowed'
  | 'conflict'
  | 'validation_failed'
  | 'rate_limited'
  | 'internal_error'
  | 'service_unavailable'
  | 'network_error'
  | 'unknown_error'

export type HealthStatus = 'ok' | 'degraded' | 'unavailable'

export interface HealthCheck {
  status: HealthStatus
  latency_ms?: number
  message?: string
}

/** GET /api/v1/health */
export interface HealthReport {
  status: HealthStatus
  service: string
  environment: string
  version: string
  uptime_seconds: number
  server_time: string
  checks: Record<string, HealthCheck>
}

/** GET /api/v1/version */
export interface VersionInfo {
  version: string
  git_commit: string
  build_time: string
  go_version: string
  environment: string
}

/** Store display modes (BLUEPRINT §7/§21). */
export type DisplayMode = 'stand' | 'handheld'

/** Supported items per page for the display grid. */
export type ItemsPerPage = 1 | 4 | 8 | 12

/** Employee availability shown on the display (BLUEPRINT §6 FG03). */
export type EmployeeStatus = 'available' | 'busy' | 'break' | 'offline'
