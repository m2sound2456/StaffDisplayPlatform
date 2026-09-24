import type { ReactNode } from 'react'

import { Badge } from '@/components/ui/Badge'
import type { DisplayMode } from '@/types/api'

export interface DisplayShellProps {
  storeSlug: string
  storeName?: string
  mode?: DisplayMode
  /** Connection indicator rendered in the bottom bar (see HealthIndicator). */
  connectionSlot?: ReactNode
  children: ReactNode
}

const modeLabels: Record<DisplayMode, string> = {
  stand: 'Stand display',
  handheld: 'Handheld display',
}

/**
 * Full-screen kiosk chrome for the tablet display (BLUEPRINT §7, §21).
 *
 * Design rules: no admin navigation, no unnecessary controls, large readable
 * type, safe areas respected, works in landscape and portrait.
 */
export function DisplayShell({ storeSlug, storeName, mode = 'stand', connectionSlot, children }: DisplayShellProps) {
  return (
    <div className="display-surface flex min-h-full flex-col bg-surface-950">
      <header className="flex items-center justify-between gap-4 px-6 pt-[max(1rem,env(safe-area-inset-top))] pb-4">
        <div className="min-w-0">
          <p className="truncate text-lg font-semibold text-slate-100 sm:text-xl">
            {storeName ?? `Store ${storeSlug}`}
          </p>
          <p className="truncate text-xs text-slate-500">/s/{storeSlug} · device pairing arrives in FG18</p>
        </div>
        <Badge tone="accent">{modeLabels[mode]}</Badge>
      </header>

      <main className="flex flex-1 items-center justify-center px-6 py-4">{children}</main>

      <footer className="flex items-center justify-between gap-4 px-6 pb-[max(1rem,env(safe-area-inset-bottom))] pt-4 text-xs text-slate-500">
        <span>Staff Display Platform</span>
        {connectionSlot}
      </footer>
    </div>
  )
}
