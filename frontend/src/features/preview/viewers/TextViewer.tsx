import { CodeView } from './CodeView'
import { decodeText, textLimit } from './text'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** Shows a text or code file read-only, highlighted by its language (ADR-0018). */
export function TextViewer({ source }: ViewerProps) {
  return <WholeFile source={source} limit={textLimit}>{(bytes) => <CodeView name={source.name} text={decodeText(bytes)} />}</WholeFile>
}
