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

/**
 * Gives the style nonce to the style elements a library adds under root, such as those of xterm.js, which cannot be given one: the
 * elements already there get it at once and those added later as they appear, and each is filled again so the browser applies it.
 * Returns the function that stops watching; nothing is watched when there is no nonce.
 */
export function nonceStyles(root: HTMLElement): () => void {
  const nonce = styleNonce()
  if (nonce === undefined) {
    return () => undefined
  }
  const grant = (style: HTMLStyleElement) => {
    if (style.nonce !== nonce) {
      const rules = style.textContent
      style.nonce = nonce
      style.textContent = rules
    }
  }
  root.querySelectorAll('style').forEach(grant)
  const observer = new MutationObserver(() => root.querySelectorAll('style').forEach(grant))
  observer.observe(root, { childList: true, subtree: true })
  return () => observer.disconnect()
}
