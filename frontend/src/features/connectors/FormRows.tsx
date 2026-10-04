import type { ReactNode } from 'react'

import { Label } from '@/components/ui/label'

/** Props of {@link Row}. */
export interface RowProps {
  /** Identifier of the input the label points at. */
  id: string
  /** Visible label, drawn in the left column. */
  label: string
  /** Validation messages, already translated; empty ones are left out. */
  errors?: ReadonlyArray<string | undefined>
  /** Explanation drawn under the input. */
  hint?: ReactNode
  /** The input, or a group of inputs. */
  children: ReactNode
}

/** One row of a form as FileZilla lays it out: the label on the left, the input and its messages on the right. */
export function Row({ id, label, errors = [], hint, children }: RowProps) {
  return (
    <div className="grid grid-cols-[8.5rem_minmax(0,1fr)] items-start gap-x-4">
      <Label htmlFor={id} className="min-h-8 font-normal text-muted-foreground">
        {label}
      </Label>
      <div className="flex min-w-0 flex-col gap-1.5">
        {children}
        {hint !== undefined && <div className="text-xs leading-relaxed text-muted-foreground">{hint}</div>}
        {errors
          .filter((error) => error !== undefined && error !== '')
          .map((error) => (
            <p key={error} className="text-xs text-destructive">
              {error}
            </p>
          ))}
      </div>
    </div>
  )
}

/** A titled group of rows inside a tab, set apart from the next one by a line. */
export function Group({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3 border-b pb-4 last:border-b-0 last:pb-0">
      {title !== undefined && <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>}
      {children}
    </section>
  )
}
