import { lazy, type ComponentType } from 'react'
import type { PreviewKind } from '../detect'
import type { ViewerProps } from './types'
/** Loads ArchiveViewer only when its file kind is opened. */
const ArchiveViewer = lazy(() => import('./ArchiveViewer').then((module) => ({ default: module.ArchiveViewer })))
/** Loads CertificateViewer only when its file kind is opened. */
const CertificateViewer = lazy(() => import('./CertificateViewer').then((module) => ({ default: module.CertificateViewer })))
/** Loads CsvViewer only when its file kind is opened. */
const CsvViewer = lazy(() => import('./CsvViewer').then((module) => ({ default: module.CsvViewer })))
/** Loads EmailViewer only when its file kind is opened. */
const EmailViewer = lazy(() => import('./EmailViewer').then((module) => ({ default: module.EmailViewer })))
/** Loads ImageViewer only when its file kind is opened. */
const ImageViewer = lazy(() => import('./ImageViewer').then((module) => ({ default: module.ImageViewer })))
/** Loads MarkdownViewer only when its file kind is opened. */
const MarkdownViewer = lazy(() => import('./MarkdownViewer').then((module) => ({ default: module.MarkdownViewer })))
/** Loads MediaViewer only when its file kind is opened. */
const MediaViewer = lazy(() => import('./MediaViewer').then((module) => ({ default: module.MediaViewer })))
/** Loads IsolatedViewer only when its file kind is opened. */
const IsolatedViewer = lazy(() => import('./IsolatedViewer').then((module) => ({ default: module.IsolatedViewer })))
/** Loads PdfViewer only when its file kind is opened. */
const PdfViewer = lazy(() => import('./PdfViewer').then((module) => ({ default: module.PdfViewer })))
/** Loads StructuredViewer only when its file kind is opened. */
const StructuredViewer = lazy(() => import('./StructuredViewer').then((module) => ({ default: module.StructuredViewer })))
/** Loads TextViewer only when its file kind is opened. */
const TextViewer = lazy(() => import('./TextViewer').then((module) => ({ default: module.TextViewer })))

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
  email: EmailViewer,
}

/** The viewer of a kind, or undefined for a kind no viewer shows, such as a binary file. */
export function viewerOf(kind: PreviewKind): ComponentType<ViewerProps> | undefined {
  return viewers[kind]
}
