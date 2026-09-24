import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError } from '@/services/apiClient'
import { fetchHealth, fetchVersion } from '@/services/healthService'
import type { HealthReport, VersionInfo } from '@/types/api'

export type ServerHealthPhase = 'loading' | 'ready' | 'error'

export interface ServerHealthState {
  phase: ServerHealthPhase
  health: HealthReport | null
  version: VersionInfo | null
  error: string | null
  /** Re-runs the status requests immediately. */
  refresh: () => void
}

/**
 * Reports the platform API status for the Admin, Setup and Display shells.
 *
 * `pollIntervalMs > 0` enables light polling (the display uses it as a
 * connection indicator). Realtime push arrives in FG22 and will replace the
 * polling for data updates.
 */
export function useServerHealth(pollIntervalMs = 0): ServerHealthState {
  const [phase, setPhase] = useState<ServerHealthPhase>('loading')
  const [health, setHealth] = useState<HealthReport | null>(null)
  const [version, setVersion] = useState<VersionInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshToken, setRefreshToken] = useState(0)

  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false

    const load = async () => {
      try {
        const [healthReport, versionInfo] = await Promise.all([
          fetchHealth({ signal: controller.signal }),
          fetchVersion({ signal: controller.signal }),
        ])
        if (cancelled) {
          return
        }
        setHealth(healthReport)
        setVersion(versionInfo)
        setError(null)
        setPhase('ready')
      } catch (caught) {
        if (cancelled || (caught instanceof DOMException && caught.name === 'AbortError')) {
          return
        }
        setError(describeFailure(caught))
        setPhase('error')
      }
    }

    void load()

    let timer: ReturnType<typeof setInterval> | undefined
    if (pollIntervalMs > 0) {
      timer = setInterval(() => {
        void load()
      }, pollIntervalMs)
    }

    return () => {
      cancelled = true
      controller.abort()
      if (timer !== undefined) {
        clearInterval(timer)
      }
    }
  }, [pollIntervalMs, refreshToken])

  const refresh = useCallback(() => {
    setPhase('loading')
    setRefreshToken((token) => token + 1)
  }, [])

  return { phase, health, version, error, refresh }
}

function describeFailure(caught: unknown): string {
  if (caught instanceof ApiError) {
    if (caught.isNetworkError) {
      return 'The Staff Display API is unreachable. Check that the backend is running.'
    }
    return `${caught.code}: ${caught.message}`
  }
  if (caught instanceof Error) {
    return caught.message
  }
  return 'Unknown error while contacting the API.'
}
