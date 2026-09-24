import { Badge, type BadgeTone } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { useServerHealth } from '@/features/health/useServerHealth'
import type { HealthStatus } from '@/types/api'

export interface HealthPanelProps {
  /** 0 disables polling (admin/setup screens refresh on demand). */
  pollIntervalMs?: number
  title?: string
}

const statusTone: Record<HealthStatus, BadgeTone> = {
  ok: 'ok',
  degraded: 'warn',
  unavailable: 'danger',
}

/**
 * Shows the connection state between the frontend and the Go API.
 * Deliberately dependency free so it can be reused by the display shell (FG10).
 */
export function HealthPanel({ pollIntervalMs = 0, title = 'Platform status' }: HealthPanelProps) {
  const { phase, health, version, error, refresh } = useServerHealth(pollIntervalMs)

  return (
    <Card
      title={title}
      description="GET /api/v1/health and /api/v1/version through the shared API client."
      actions={
        <button
          type="button"
          onClick={refresh}
          className="rounded-lg border border-surface-700 px-3 py-1.5 text-xs font-medium text-slate-200 transition hover:border-accent-400 hover:text-accent-300"
        >
          Refresh
        </button>
      }
    >
      {phase === 'loading' && <p className="text-sm text-slate-400">Checking the API…</p>}

      {phase === 'error' && (
        <div className="space-y-2">
          <Badge tone="danger">unreachable</Badge>
          <p role="alert" className="text-sm text-red-300">
            {error}
          </p>
          <p className="text-xs text-slate-500">
            Start the backend with <code className="text-slate-300">go run ./cmd/server</code> (default
            http://127.0.0.1:8080).
          </p>
        </div>
      )}

      {phase === 'ready' && health !== null && (
        <dl className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">API</dt>
            <dd className="mt-1">
              <Badge tone={statusTone[health.status]}>{health.status}</Badge>
            </dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">Service</dt>
            <dd className="mt-1 text-slate-200">{health.service}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">Environment</dt>
            <dd className="mt-1 text-slate-200">{health.environment}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">Version</dt>
            <dd className="mt-1 text-slate-200">
              {version?.version ?? health.version}
              {version !== null && version.git_commit !== 'unknown' && (
                <span className="ml-2 text-xs text-slate-500">{version.git_commit}</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">Database</dt>
            <dd className="mt-1 text-slate-200">
              {describeDatabase(health.checks['database']?.status, health.checks['database']?.latency_ms)}
            </dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500">Uptime</dt>
            <dd className="mt-1 text-slate-200">{formatUptime(health.uptime_seconds)}</dd>
          </div>
        </dl>
      )}
    </Card>
  )
}

function describeDatabase(status: HealthStatus | undefined, latencyMs: number | undefined): string {
  if (status === undefined) {
    return 'not reported'
  }
  if (status === 'ok') {
    return latencyMs === undefined ? 'connected' : `connected (${latencyMs.toFixed(1)} ms)`
  }
  return status
}

function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return '—'
  }
  const total = Math.floor(seconds)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const secs = total % 60
  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }
  if (minutes > 0) {
    return `${minutes}m ${secs}s`
  }
  return `${secs}s`
}
