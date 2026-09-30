import type { PreviewKind } from '../detect'
import type { PreviewSource } from '../source'

/** What every viewer receives: the file to show and the kind it was told to be. */
export interface ViewerProps {
  /** The file to show. */
  source: PreviewSource
  /** The kind of the file. */
  kind: PreviewKind
}
