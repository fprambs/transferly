import type { ComponentType } from 'react'

import type { PreviewKind } from '../detect'

import { CsvViewer } from './CsvViewer'
import { HexViewer } from './HexViewer'
import { MarkdownViewer } from './MarkdownViewer'
import { StructuredViewer } from './StructuredViewer'
import { TextViewer } from './TextViewer'
import type { ViewerProps } from './types'

export type { ViewerProps } from './types'

/** The viewer of each kind that has one; each stage of D2 adds its own (ADR-0018). */
const viewers: Partial<Record<PreviewKind, ComponentType<ViewerProps>>> = {
  binary: HexViewer,
  text: TextViewer,
  markdown: MarkdownViewer,
  json: StructuredViewer,
  yaml: StructuredViewer,
  xml: StructuredViewer,
  csv: CsvViewer,
}

/** The viewer of a kind, and whether it is the hexadecimal fallback standing in for a viewer that does not exist yet. */
export function viewerOf(kind: PreviewKind): { View: ComponentType<ViewerProps>; fallback: boolean } {
  const View = viewers[kind]
  return View === undefined ? { View: HexViewer, fallback: true } : { View, fallback: false }
}
