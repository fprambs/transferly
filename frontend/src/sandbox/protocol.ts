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
  /** The first bytes of the file, or all of them, transferred. */
  bytes: ArrayBuffer
  /** The size of the whole file, in bytes; a renderer that needs more than the first bytes asks for ranges. */
  size: number
  /** The theme tokens of the parent, as CSS custom properties. */
  theme: Record<string, string>
  /** The words of the interface the renderer shows, translated by the parent, which the page has no translations of its own for. */
  labels: Record<string, string>
}

/** Sent by the page: asks for the bytes from `begin` included to `end` excluded of the file being shown. */
export interface RangeRequest {
  /** Names the message. */
  type: 'sentinel:range'
  /** Identifies the request, so the answer finds it. */
  id: number
  /** First byte asked for. */
  begin: number
  /** The byte after the last one asked for. */
  end: number
}

/** Sent by the parent: the bytes of a range the page asked for, or `null` when they could not be read. */
export interface RangeReply {
  /** Names the message. */
  type: 'sentinel:bytes'
  /** The request answered. */
  id: number
  /** The bytes, transferred. */
  bytes: ArrayBuffer | null
}

/** Sent by the page: how the file went, and why it failed when it did. */
export interface ResultMessage {
  /** Names the message: the file was shown, or could not be. */
  type: 'sentinel:rendered' | 'sentinel:failed'
  /** A short reason, never shown as HTML. */
  reason?: string
}

/** Sent by the page: a key the viewer around it handles (Escape, and the arrows outside a field), pressed while the focus was inside the
 * page, where the viewer cannot hear it. */
export interface KeyMessage {
  /** Names the message. */
  type: 'sentinel:key'
  /** The key, as `KeyboardEvent.key` names it. */
  key: string
}

/** Every message the page sends. */
export type PageMessage = ReadyMessage | ResultMessage | RangeRequest | KeyMessage

/** Tells whether data received by the page is a render message. */
export function isRenderMessage(data: unknown): data is RenderMessage {
  const message = data as Partial<RenderMessage> | null
  return (
    typeof message === 'object' &&
    message !== null &&
    message.type === 'sentinel:render' &&
    typeof message.kind === 'string' &&
    typeof message.name === 'string' &&
    message.bytes instanceof ArrayBuffer &&
    typeof message.size === 'number'
  )
}

/** Tells whether data received by the page is the answer to a range it asked for. */
export function isRangeReply(data: unknown): data is RangeReply {
  const message = data as Partial<RangeReply> | null
  return typeof message === 'object' && message !== null && message.type === 'sentinel:bytes' && typeof message.id === 'number' && (message.bytes === null || message.bytes instanceof ArrayBuffer)
}

/** Tells whether data received by the parent is a message of the page. */
export function isPageMessage(data: unknown): data is PageMessage {
  const message = data as Partial<PageMessage> | null
  return typeof message === 'object' && message !== null && ['sentinel:ready', 'sentinel:rendered', 'sentinel:failed', 'sentinel:range', 'sentinel:key'].includes(String(message.type))
}
