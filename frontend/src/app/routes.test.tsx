import { render, screen } from '@testing-library/react'
import { describe, expect, it, beforeEach } from 'vitest'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'

import { routes } from '@/app/routes'
import { stubHealthApi } from '@/test/mocks'

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return render(<RouterProvider router={router} />)
}

describe('application routes', () => {
  beforeEach(() => {
    stubHealthApi()
  })

  it('renders the landing page with the API status', async () => {
    renderAt('/')

    expect(await screen.findByRole('heading', { name: /one platform for every store display/i })).toBeInTheDocument()
    expect(await screen.findByText('staffdisplay-api')).toBeInTheDocument()
    expect(screen.getByText('/s/{store-slug}')).toBeInTheDocument()
  })

  it('renders the store display shell for a valid slug', async () => {
    renderAt('/s/coffee')

    expect(await screen.findByRole('heading', { name: '/s/coffee' })).toBeInTheDocument()
    expect(screen.getByText(/no subdomain, no per-store site/i)).toBeInTheDocument()
    expect(screen.getByText(/stand display/i)).toBeInTheDocument()
  })

  it('keeps display deep links working', async () => {
    renderAt('/s/coffee/playlist')

    expect(await screen.findByRole('heading', { name: '/s/coffee' })).toBeInTheDocument()
  })

  it('rejects a slug that violates the rules', () => {
    renderAt('/s/INVALID')

    expect(screen.getByRole('heading', { name: /invalid store url/i })).toBeInTheDocument()
    expect(screen.getByText(/must be lowercase/i)).toBeInTheDocument()
  })

  it('rejects a reserved slug', () => {
    renderAt('/s/setup')

    expect(screen.getByRole('heading', { name: /invalid store url/i })).toBeInTheDocument()
    expect(screen.getByText(/reserved by the platform/i)).toBeInTheDocument()
  })

  it('renders the admin shell for /app and deep links', async () => {
    renderAt('/app/employees')

    expect(await screen.findByRole('heading', { name: /admin application/i })).toBeInTheDocument()
    expect(screen.getByText(/FG4 authentication pending/i)).toBeInTheDocument()
  })

  it('renders the device setup flow', () => {
    renderAt('/setup')

    expect(screen.getByRole('heading', { name: /pair a tablet/i })).toBeInTheDocument()
    expect(screen.getByText(/admin password is never stored on the tablet/i)).toBeInTheDocument()
  })

  it('renders the not found screen for unknown paths', () => {
    renderAt('/does-not-exist')

    expect(screen.getByRole('heading', { name: /screen not found/i })).toBeInTheDocument()
  })
})
