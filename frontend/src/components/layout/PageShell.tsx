import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'

export interface PageShellProps {
  eyebrow?: string
  title: string
  subtitle?: string
  actions?: ReactNode
  children: ReactNode
}

const navLinkClass = ({ isActive }: { isActive: boolean }): string =>
  isActive ? 'text-accent-300' : 'text-slate-300 transition hover:text-accent-300'

/** Shared chrome for the landing, admin and device setup screens. */
export function PageShell({ eyebrow, title, subtitle, actions, children }: PageShellProps) {
  return (
    <div className="min-h-full">
      <header className="border-b border-surface-800 bg-surface-900/70 backdrop-blur">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-5 py-4">
          <Link to="/" className="flex items-center gap-3">
            <img src="/favicon.svg" alt="" width={36} height={36} className="h-9 w-9" />
            <span className="leading-tight">
              <span className="block text-sm font-semibold text-slate-100">Staff Display Platform</span>
              <span className="block text-xs text-slate-500">one platform · many stores</span>
            </span>
          </Link>
          <nav className="flex items-center gap-5 text-sm" aria-label="Primary">
            <NavLink to="/app" className={navLinkClass}>
              Admin
            </NavLink>
            <NavLink to="/setup" className={navLinkClass}>
              Device setup
            </NavLink>
          </nav>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-5 py-8">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            {eyebrow !== undefined && (
              <p className="text-xs font-medium uppercase tracking-[0.2em] text-accent-400">{eyebrow}</p>
            )}
            <h1 className="mt-1 text-2xl font-semibold text-slate-50 sm:text-3xl">{title}</h1>
            {subtitle !== undefined && <p className="mt-2 max-w-2xl text-sm text-slate-400">{subtitle}</p>}
          </div>
          {actions !== undefined && <div className="flex items-center gap-2">{actions}</div>}
        </div>
        <div className="space-y-5">{children}</div>
      </main>

      <footer className="mx-auto max-w-6xl px-5 pb-10 text-xs text-slate-600">
        FG1 foundation · roadmap in <code className="text-slate-500">docs/BLUEPRINT.md</code> · API contract in{' '}
        <code className="text-slate-500">docs/API.md</code>
      </footer>
    </div>
  )
}
