import { Badge } from '@/components/ui/Badge'
import { useServerHealth } from '@/features/health/useServerHealth'

export interface ConnectionIndicatorProps {
  pollIntervalMs?: number
}

/**
 * Compact online/offline indicator for the display footer. Offline display from
 * cache is FG29; until then the indicator reports connectivity only.
 */
export function ConnectionIndicator({ pollIntervalMs = 15000 }: ConnectionIndicatorProps) {
  const { phase, health } = useServerHealth(pollIntervalMs)

  if (phase === 'loading') {
    return (
      <span className="flex items-center gap-2">
        <span className="h-2 w-2 animate-pulse rounded-full bg-slate-400" aria-hidden="true" />
        connecting
      </span>
    )
  }

  if (phase === 'error') {
    return (
      <span className="flex items-center gap-2 text-amber-300">
        <span className="h-2 w-2 rounded-full bg-amber-400" aria-hidden="true" />
        offline
      </span>
    )
  }

  const database = health?.checks['database']?.status

  return (
    <span className="flex items-center gap-2">
      <span className="h-2 w-2 rounded-full bg-emerald-400" aria-hidden="true" />
      online
      {database !== undefined && (
        <Badge tone={database === 'ok' ? 'ok' : 'warn'} className="ml-1">
          db {database}
        </Badge>
      )}
    </span>
  )
}
