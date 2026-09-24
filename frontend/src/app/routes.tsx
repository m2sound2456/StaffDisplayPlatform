import { createBrowserRouter, type RouteObject } from 'react-router-dom'

import { AdminAppPage } from '@/pages/AdminAppPage'
import { DeviceSetupPage } from '@/pages/DeviceSetupPage'
import { LandingPage } from '@/pages/LandingPage'
import { NotFoundPage } from '@/pages/NotFoundPage'
import { StoreDisplayPage } from '@/pages/StoreDisplayPage'

/**
 * Application routes — single domain + path based tenants (BLUEPRINT §4, §32).
 *
 *   /                landing
 *   /app             admin (deep links such as /app/employees keep working)
 *   /s/{store-slug}  store display for one tenant
 *   /setup           device pairing
 *
 * Exported as data so tests can mount the same table in a memory router.
 */
export const routes: RouteObject[] = [
  { path: '/', element: <LandingPage /> },
  { path: '/app', element: <AdminAppPage /> },
  { path: '/app/*', element: <AdminAppPage /> },
  { path: '/s/:storeSlug', element: <StoreDisplayPage /> },
  { path: '/s/:storeSlug/*', element: <StoreDisplayPage /> },
  { path: '/setup', element: <DeviceSetupPage /> },
  { path: '*', element: <NotFoundPage /> },
]

export function createAppRouter() {
  return createBrowserRouter(routes)
}
