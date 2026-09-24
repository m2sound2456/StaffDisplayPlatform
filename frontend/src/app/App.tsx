import { RouterProvider } from 'react-router-dom'

import { createAppRouter } from '@/app/routes'

// A single browser router instance for the lifetime of the tab.
const router = createAppRouter()

/** Root component: Admin, Store Display and Device Setup share one SPA. */
export function App() {
  return <RouterProvider router={router} />
}
