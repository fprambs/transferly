import { EditorView } from '@codemirror/view'
import type { Extension } from '@codemirror/state'

/**
 * Returns the nonce the server gave this page for the styles it injects at run time, or `undefined` in development, where the page is
 * served without the policy and the placeholder is left as it is.
 */
export function styleNonce(): string | undefined {
  const nonce = document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content
  return nonce === undefined || nonce === '' || nonce.startsWith('__') ? undefined : nonce
}

/**
 * The extension that hands the style nonce to CodeMirror, whose theme styles would otherwise be refused by the Content-Security-Policy of
 * the page; empty when there is no nonce.
 */
export function editorNonce(): Extension {
  const nonce = styleNonce()
  return nonce === undefined ? [] : EditorView.cspNonce.of(nonce)
}
