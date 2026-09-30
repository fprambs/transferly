/**
 * The messages between the explorer and the isolated page of the file viewers (ADR-0018). The page has an opaque origin, so the parent
 * accepts only messages whose source is its own frame, and the page only messages from its parent.
 */

/** Sent by the page once it listens: the parent may send the file. */
export interface ReadyMessage {
  /** Names the message. */
  type: 'sentinel:ready'
}

/** Sent by the parent: the file to show, its kind and the colors of the theme. */
export interface RenderMessage {
  /** Names the message. */
  type: 'sentinel:render'
  /** The kind the file was told to be. */
  kind: string
  /** Name of the file. */
  name: string
  /** The bytes of the file, transferred. */
  bytes: ArrayBuffer
  /** The theme tokens of the parent, as CSS custom properties. */
  theme: Record<string, string>
}

/** Sent by the page: how the file went, and why it failed when it did. */
export interface ResultMessage {
  /** Names the message: the file was shown, or could not be. */
  type: 'sentinel:rendered' | 'sentinel:failed'
  /** A short reason, never shown as HTML. */
  reason?: string
}

/** Every message the page sends. */
export type PageMessage = ReadyMessage | ResultMessage

/** Tells whether data received by the page is a render message. */
export function isRenderMessage(data: unknown): data is RenderMessage {
  const message = data as Partial<RenderMessage> | null
  return typeof message === 'object' && message !== null && message.type === 'sentinel:render' && typeof message.kind === 'string' && typeof message.name === 'string' && message.bytes instanceof ArrayBuffer
}

/** Tells whether data received by the parent is a message of the page. */
export function isPageMessage(data: unknown): data is PageMessage {
  const message = data as Partial<PageMessage> | null
  return typeof message === 'object' && message !== null && ['sentinel:ready', 'sentinel:rendered', 'sentinel:failed'].includes(String(message.type))
}
