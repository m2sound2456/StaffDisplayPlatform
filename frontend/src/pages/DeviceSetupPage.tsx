import { Link } from 'react-router-dom'

import { PageShell } from '@/components/layout/PageShell'
import { Badge } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'

const pairingSteps = [
  { step: '1', title: 'Admin signs in', detail: 'Store admin opens /app on a phone or PC.', group: 'FG4' },
  {
    step: '2',
    title: 'Create the store',
    detail: 'A store record (id, slug, name) publishes /s/{slug} instantly.',
    group: 'FG5',
  },
  {
    step: '3',
    title: 'Add a device',
    detail: 'Register Tablet-01/02/03 and its own display configuration.',
    group: 'FG16',
  },
  {
    step: '4',
    title: 'Generate QR / pairing code',
    detail: 'Short lived, single use, scoped to the store.',
    group: 'FG17',
  },
  { step: '5', title: 'Open /setup on the tablet', detail: 'Scan the QR or type the pairing code.', group: 'FG18' },
  {
    step: '6',
    title: 'Device receives credentials',
    detail: 'Device id + token; the admin password is never stored on the tablet.',
    group: 'FG19',
  },
  { step: '7', title: 'Display starts', detail: 'The tablet fetches its own store data only.', group: 'FG10' },
]

/** Device setup / pairing entry point for tablets (-placeholder until FG16–FG19). */
export function DeviceSetupPage() {
  return (
    <PageShell
      eyebrow="Device setup"
      title="Pair a tablet"
      subtitle="A tablet gets its own identity and credential. It never receives store admin credentials, and it can only ever see its own store."
      actions={<Badge tone="warn">FG16–FG18 pending</Badge>}
    >
      <Card title="Pairing flow" description="Admin → Store → Device → QR → Pair → Display (BLUEPRINT §10).">
        <ol className="space-y-3">
          {pairingSteps.map((entry) => (
            <li key={entry.step} className="flex gap-4 rounded-xl border border-surface-800 bg-surface-900/50 p-4">
              <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-800 text-sm font-semibold text-accent-300">
                {entry.step}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-slate-100">{entry.title}</span>
                  <Badge tone="neutral">{entry.group}</Badge>
                </div>
                <p className="mt-1 text-xs text-slate-400">{entry.detail}</p>
              </div>
            </li>
          ))}
        </ol>
      </Card>

      <Card title="What already exists" description="FG1 foundation.">
        <ul className="space-y-2 text-sm text-slate-300">
          <li>
            Device scoped API routing is reserved on the server (
            <code className="text-accent-300">/api/v1/display/*</code>,{' '}
            <code className="text-accent-300">/api/v1/devices/*</code>).
          </li>
          <li>
            The display URL is stable and per-tenant: <code className="text-accent-300">/s/{'{store-slug}'}</code>.
          </li>
          <li>Tenant isolation and revocation tests are scheduled with their features (BLUEPRINT §26).</li>
        </ul>
        <p className="mt-4 text-sm">
          <Link className="text-accent-300 hover:text-accent-200" to="/">
            ← Back to the platform overview
          </Link>
        </p>
      </Card>
    </PageShell>
  )
}
