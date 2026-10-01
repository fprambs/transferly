import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

/** A table of a card: full width, scrolling sideways on narrow screens instead of squeezing its columns. */
export function DataTable({ className, ...props }: ComponentProps<'table'>) {
  return (
    <div className="overflow-x-auto">
      <table className={cn('w-full border-collapse text-[13px]', className)} {...props} />
    </div>
  )
}

/** A heading cell: small capitals on a tinted band, aligned to the start unless told otherwise. */
export function Th({ className, ...props }: ComponentProps<'th'>) {
  return <th scope="col" className={cn('border-b bg-muted/40 px-5 py-2.5 text-start text-[11px] font-medium tracking-wider whitespace-nowrap text-muted-foreground uppercase', className)} {...props} />
}

/** A row of the body, highlighted under the pointer and divided from the next one. */
export function Tr({ className, ...props }: ComponentProps<'tr'>) {
  return <tr className={cn('border-b transition-colors last:border-b-0 hover:bg-muted/30', className)} {...props} />
}

/** A cell of the body. */
export function Td({ className, ...props }: ComponentProps<'td'>) {
  return <td className={cn('px-5 py-3 align-middle', className)} {...props} />
}
