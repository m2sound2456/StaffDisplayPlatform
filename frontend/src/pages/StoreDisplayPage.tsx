import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { DisplayShell } from '@/components/layout/DisplayShell'
import { PageShell } from '@/components/layout/PageShell'
import { Badge } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { ConnectionIndicator } from '@/features/health/ConnectionIndicator'
import { validateStoreSlug } from '@/lib/storeSlug'
import type { ItemsPerPage } from '@/types/api'

const itemsPerPageOptions: readonly ItemsPerPage[] = [1, 4, 8, 12]

/**
 * Store display screen (tablet) — /s/{store-slug}.
 *
 * FG1 delivers the shell, the tenant resolution by slug and the kiosk layout.
 * Staff slides, promotions, QR slides and the playlist follow in FG10–FG15; the
 * per-device configuration (stand/handheld, items per page, auto slide, loop,
 * employee status) is stored per device from FG16 and applied here from FG11.
 */
export function StoreDisplayPage() {
  const { storeSlug } = useParams<{ storeSlug: string }>()
  const validation = validateStoreSlug(storeSlug)
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(timer)
  }, [])

  if (!validation.valid || storeSlug === undefined) {
    return (
      <PageShell eyebrow="Display" title="Invalid store URL">
        <Card title="This store link cannot be resolved" description={validation.reason}>
          <p className="text-sm text-slate-300">
            Store URLs look like <code className="text-accent-300">/s/coffee</code>. Slugs are validated in the same way
            on the client and on the API.
          </p>
          <p className="mt-3 text-sm">
            <Link className="text-accent-300 hover:text-accent-200" to="/">
              ← Back to the platform overview
            </Link>
          </p>
        </Card>
      </PageShell>
    )
  }

  return (
    <DisplayShell storeSlug={storeSlug} mode="stand" connectionSlot={<ConnectionIndicator />}>
      <div className="w-full max-w-4xl text-center">
        <p className="text-xs uppercase tracking-[0.3em] text-slate-500">Store display</p>
        <h1 className="mt-3 text-3xl font-semibold text-slate-50 sm:text-4xl">/s/{storeSlug}</h1>
        <p className="mt-3 text-sm text-slate-400">
          Tenant resolved from the URL path — no subdomain, no per-store site, no per-store database.
        </p>

        <div className="mt-8 grid gap-4 text-left sm:grid-cols-3">
          <Card title="Stand mode" description="Storefront tablet">
            <p className="text-xs text-slate-400">Auto slide, configurable interval, loop on/off, 4/8/12 items.</p>
            <Badge tone="accent" className="mt-3">
              FG11–FG12
            </Badge>
          </Card>
          <Card title="Handheld mode" description="Staff showing profiles">
            <p className="text-xs text-slate-400">
              One employee per page, manual swipe / next / previous, auto slide off.
            </p>
            <Badge tone="accent" className="mt-3">
              FG11–FG12
            </Badge>
          </Card>
          <Card title="Page size" description="Supported grid sizes">
            <p className="text-xs text-slate-400">{itemsPerPageOptions.join(' / ')} employees per page.</p>
            <Badge tone="neutral" className="mt-3">
              per device
            </Badge>
          </Card>
        </div>

        <p className="mt-8 font-mono text-xs text-slate-500">{now.toLocaleString()}</p>
      </div>
    </DisplayShell>
  )
}
