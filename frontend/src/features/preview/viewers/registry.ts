import type { ComponentType } from 'react'

import type { PreviewKind } from '../detect'

import { ArchiveViewer } from './ArchiveViewer'
import { CertificateViewer } from './CertificateViewer'
import { CsvViewer } from './CsvViewer'
import { ImageViewer } from './ImageViewer'
import { MarkdownViewer } from './MarkdownViewer'
import { MediaViewer } from './MediaViewer'
import { IsolatedViewer } from './IsolatedViewer'
import { PdfViewer } from './PdfViewer'
import { StructuredViewer } from './StructuredViewer'
import { TextViewer } from './TextViewer'
import type { ViewerProps } from './types'

export type { ViewerProps } from './types'

/** The viewer of each kind that has one; each stage of D2 adds its own (ADR-0018). */
const viewers: Partial<Record<PreviewKind, ComponentType<ViewerProps>>> = {
  text: TextViewer,
  markdown: MarkdownViewer,
  json: StructuredViewer,
  yaml: StructuredViewer,
  xml: StructuredViewer,
  csv: CsvViewer,
  image: ImageViewer,
  svg: ImageViewer,
  audio: MediaViewer,
  video: MediaViewer,
  pdf: PdfViewer,
  archive: ArchiveViewer,
  document: IsolatedViewer,
  spreadsheet: IsolatedViewer,
  presentation: IsolatedViewer,
  sqlite: IsolatedViewer,
  font: IsolatedViewer,
  certificate: CertificateViewer,
}

/** The viewer of a kind, or undefined for a kind no viewer shows, such as a binary file. */
export function viewerOf(kind: PreviewKind): ComponentType<ViewerProps> | undefined {
  return viewers[kind]
}
