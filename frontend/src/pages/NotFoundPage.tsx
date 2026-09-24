import { Link, useLocation } from 'react-router-dom'

import { PageShell } from '@/components/layout/PageShell'
import { Card } from '@/components/ui/Card'

/** 404 screen — keeps kiosk/tablet users from landing on a blank page. */
export function NotFoundPage() {
  const location = useLocation()

  return (
    <PageShell eyebrow="404" title="Screen not found">
      <Card title="Unknown route" description={location.pathname}>
        <p className="text-sm text-slate-300">
          Valid screens are <code className="text-accent-300">/</code>, <code className="text-accent-300">/app</code>,{' '}
          <code className="text-accent-300">/s/{'{store-slug}'}</code> and{' '}
          <code className="text-accent-300">/setup</code>.
        </p>
        <p className="mt-4 text-sm">
          <Link className="text-accent-300 hover:text-accent-200" to="/">
            ← Back to the platform overview
          </Link>
        </p>
      </Card>
    </PageShell>
  )
}
