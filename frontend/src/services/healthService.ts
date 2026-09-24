import { api, type RequestOptions } from '@/services/apiClient'
import type { HealthReport, VersionInfo } from '@/types/api'

/**
 * Platform status endpoints (implemented in FG1).
 *
 * GET /api/v1/health  → detailed dependency report (envelope: HealthReport)
 * GET /api/v1/version → build metadata
 */

export function fetchHealth(options?: Omit<RequestOptions, 'method' | 'body'>): Promise<HealthReport> {
  return api.get<HealthReport>('/health', options)
}

export function fetchVersion(options?: Omit<RequestOptions, 'method' | 'body'>): Promise<VersionInfo> {
  return api.get<VersionInfo>('/version', options)
}
