import type { ReactNode } from 'react'

export type BadgeTone = 'ok' | 'warn' | 'danger' | 'neutral' | 'accent'

const toneClasses: Record<BadgeTone, string> = {
  ok: 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/30',
  warn: 'bg-amber-500/15 text-amber-200 ring-amber-500/30',
  danger: 'bg-red-500/15 text-red-300 ring-red-500/30',
  neutral: 'bg-slate-500/15 text-slate-300 ring-slate-500/30',
  accent: 'bg-cyan-500/15 text-cyan-300 ring-cyan-500/30',
}

export interface BadgeProps {
  tone?: BadgeTone
  children: ReactNode
  className?: string
}

/** Small status pill used across the admin, setup and display screens. */
export function Badge({ tone = 'neutral', children, className }: BadgeProps) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset',
        toneClasses[tone],
        className ?? '',
      ].join(' ')}
    >
      {children}
    </span>
  )
}
