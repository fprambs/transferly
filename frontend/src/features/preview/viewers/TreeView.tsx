import { ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { TreeNode } from './tree'

/** How many children an open node draws before offering the rest. */
const pageSize = 200

/** Colors of the values of each type. */
const valueTones: Partial<Record<TreeNode['type'], string>> = {
  string: 'text-ok',
  number: 'text-info',
  boolean: 'text-warn',
  null: 'text-muted-foreground',
  text: 'text-foreground',
}

/** One node and, when open, its children. The first three levels start open. */
function Node({ node, depth }: { node: TreeNode; depth: number }) {
  const { t } = useTranslation('preview')
  const [open, setOpen] = useState(depth < 3)
  const [shown, setShown] = useState(pageSize)
  const children = node.children ?? []
  const branch = node.children !== undefined
  const summary = node.type === 'array' ? `[${children.length}]` : node.type === 'object' ? `{${children.length}}` : ''
  return (
    <li role="treeitem" aria-expanded={branch ? open : undefined} aria-selected={false} className="font-mono text-xs leading-6">
      <div className="flex items-baseline gap-1.5" style={{ paddingInlineStart: depth * 16 }}>
        {branch ? (
          <button type="button" aria-label={open ? t(($) => $.tree.collapse) : t(($) => $.tree.expand)} className="self-center rounded hover:bg-muted" onClick={() => setOpen(!open)}>
            <ChevronRight aria-hidden="true" className={cn('size-3.5 transition-transform', open && 'rotate-90')} />
          </button>
        ) : (
          <span className="w-3.5" />
        )}
        {node.type === 'element' ? (
          <span>
            <span className="text-accent-text">&lt;{node.label}</span>
            {(node.attributes as Array<[string, string]>).map(([name, value]) => (
              <span key={name}>
                {' '}
                <span className="text-info">{name}</span>=<span className="text-ok">&quot;{value}&quot;</span>
              </span>
            ))}
            <span className="text-accent-text">&gt;</span>
          </span>
        ) : (
          node.label !== '' && <span className="text-accent-text">{node.label}:</span>
        )}
        {summary !== '' && <span className="text-muted-foreground">{summary}</span>}
        {node.value !== undefined && <span className={cn('break-all', valueTones[node.type])}>{node.type === 'string' ? `"${node.value}"` : node.value}</span>}
      </div>
      {branch && open && (
        <ul role="group">
          {children.slice(0, shown).map((child, index) => (
            <Node key={index} node={child} depth={depth + 1} />
          ))}
          {children.length > shown && (
            <li role="none" style={{ paddingInlineStart: (depth + 1) * 16 + 20 }}>
              <button type="button" className="text-xs text-accent-text hover:underline" onClick={() => setShown(shown + pageSize)}>
                {t(($) => $.tree.more, { count: children.length - shown })}
              </button>
            </li>
          )}
        </ul>
      )}
    </li>
  )
}

/** Draws a structured file as a tree whose nodes open and close; long lists are drawn by pages of 200. */
export function TreeView({ root, label }: { root: TreeNode; label: string }) {
  return (
    <ul role="tree" aria-label={label} className="min-h-0 flex-1 overflow-auto bg-card p-3">
      <Node node={root} depth={0} />
    </ul>
  )
}
