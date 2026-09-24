import { PageShell } from '@/components/layout/PageShell'
import { Badge } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { HealthPanel } from '@/features/health/HealthPanel'

const modules = [
  { name: 'Dashboard', detail: 'Employees, devices, online/offline counts', group: 'FG5+' },
  { name: 'Store', detail: 'Profile, slug, timezone, logo, status', group: 'FG5' },
  { name: 'Employees', detail: 'CRUD, images, availability, display order', group: 'FG6–FG9' },
  { name: 'Display', detail: 'Playlist, per-device settings, preview', group: 'FG10–FG15' },
  { name: 'Devices', detail: 'Register, pair by QR/code, revoke, last seen', group: 'FG16–FG21' },
  { name: 'Media', detail: 'Employee and promotion assets (local → object storage)', group: 'FG7' },
]

/**
 * Admin application shell (FG1 placeholder).
 *
 * Authentication (FG4) and the tenant/store model (FG5) land next; the routes
 * under /app keep working for deep links in the meantime.
 */
export function AdminAppPage() {
  return (
    <PageShell
      eyebrow="Admin"
      title="Admin application"
      subtitle="Store admins manage their own store only — super admins manage the platform. Sign-in arrives with FG4."
      actions={<Badge tone="warn">FG4 authentication pending</Badge>}
    >
      <HealthPanel />

      <Card title="Modules" description="Planned admin areas from BLUEPRINT §20.">
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {modules.map((module) => (
            <li key={module.name} className="rounded-xl border border-surface-800 bg-surface-900/60 p-4">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium text-slate-100">{module.name}</span>
                <Badge tone="neutral">{module.group}</Badge>
              </div>
              <p className="mt-2 text-xs text-slate-400">{module.detail}</p>
            </li>
          ))}
        </ul>
      </Card>

      <Card title="Tenant isolation" description="Non-negotiable rule enforced by the API layer.">
        <p className="text-sm text-slate-300">
          Every query is scoped by the authenticated store/device identity. Requesting another store’s resource returns{' '}
          <code className="text-accent-300">404 not_found</code> so nothing leaks, and the isolation tests from
          BLUEPRINT §26 run in CI from FG5 onwards.
        </p>
      </Card>
    </PageShell>
  )
}
