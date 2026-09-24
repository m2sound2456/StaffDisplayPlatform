import { Link } from 'react-router-dom'

import { PageShell } from '@/components/layout/PageShell'
import { Badge } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { HealthPanel } from '@/features/health/HealthPanel'
import { defaultStoreSlug } from '@/lib/storeSlug'

const routeMap = [
  { path: '/app', label: 'Admin application', detail: 'Stores, employees, display settings, devices and pairing.' },
  { path: '/s/{store-slug}', label: 'Store display', detail: 'Tablet screen: stand mode and handheld mode.' },
  { path: '/setup', label: 'Device setup', detail: 'Pair a new tablet with its own device credential.' },
  { path: '/api/v1', label: 'REST API', detail: 'Single Go API, tenant scoped, same origin.' },
]

/** Public landing page: explains the platform and proves the stack is wired up. */
export function LandingPage() {
  const demoSlug = defaultStoreSlug()

  return (
    <PageShell
      eyebrow="FG1 · foundation"
      title="One platform for every store display"
      subtitle="Multi-tenant staff displays for storefront tablets: profiles, availability, promotions and QR content — served from a single application, database and domain."
      actions={
        <>
          <Link
            to={`/s/${demoSlug}`}
            className="rounded-lg bg-accent-500 px-4 py-2 text-sm font-semibold text-surface-950 transition hover:bg-accent-400"
          >
            Open demo display
          </Link>
          <Link
            to="/setup"
            className="rounded-lg border border-surface-700 px-4 py-2 text-sm font-medium text-slate-200 transition hover:border-accent-400 hover:text-accent-300"
          >
            Pair a tablet
          </Link>
        </>
      }
    >
      <HealthPanel />

      <Card title="Routing model" description="Single domain + path based tenants (BLUEPRINT §32).">
        <ul className="divide-y divide-surface-800">
          {routeMap.map((route) => (
            <li key={route.path} className="flex flex-wrap items-center justify-between gap-2 py-3">
              <div>
                <code className="text-sm text-accent-300">{route.path}</code>
                <p className="text-xs text-slate-500">{route.detail}</p>
              </div>
              <span className="text-sm text-slate-300">{route.label}</span>
            </li>
          ))}
        </ul>
      </Card>

      <Card title="Multi-tenant checklist" description="Invariants that hold for every feature group.">
        <ul className="grid gap-2 text-sm text-slate-300 sm:grid-cols-2">
          {[
            'One database, tenant_id / store_id on every business row',
            'Store admin can never read another store’s data',
            'Every tablet has its own device identity and token',
            'Display configuration is per device (mode, page size, timing)',
            'No ESP32, no sensors, no external hardware',
            'Web/PWA only: works in landscape and portrait',
          ].map((item) => (
            <li key={item} className="flex items-start gap-2">
              <Badge tone="ok">✓</Badge>
              <span>{item}</span>
            </li>
          ))}
        </ul>
      </Card>
    </PageShell>
  )
}
