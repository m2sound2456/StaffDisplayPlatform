import type { ReactNode } from 'react'

export interface CardProps {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  children?: ReactNode
  className?: string
}

/** Surface used by every non-display screen. */
export function Card({ title, description, actions, children, className }: CardProps) {
  return (
    <section
      className={[
        'rounded-2xl border border-surface-700/60 bg-surface-900/80 p-5 shadow-lg shadow-black/20',
        className ?? '',
      ].join(' ')}
    >
      {(title !== undefined || actions !== undefined) && (
        <header className="mb-4 flex items-start justify-between gap-4">
          <div>
            {title !== undefined && <h2 className="text-base font-semibold text-slate-100">{title}</h2>}
            {description !== undefined && <p className="mt-1 text-sm text-slate-400">{description}</p>}
          </div>
          {actions !== undefined && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </header>
      )}
      {children}
    </section>
  )
}
