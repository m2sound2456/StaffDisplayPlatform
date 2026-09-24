import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from '@/app/App'
import { registerServiceWorker } from '@/pwa/registerServiceWorker'
import '@/styles/index.css'

const container = document.getElementById('root')

if (container === null) {
  throw new Error('Missing #root container in index.html')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// PWA: offline shell + installable tablet app (FG27–FG30 extend the data cache).
registerServiceWorker()
