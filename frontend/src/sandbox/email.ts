import DOMPurify from 'dompurify'

import { dropRemote } from './remote'

/**
 * Draws the HTML body of a message inside the isolated page (ADR-0018). DOMPurify keeps only HTML and styles, without scripts, handlers,
 * forms, frames or embedded objects; remote addresses of pictures and CSS are removed, so a tracking picture is not even asked for, and the
 * policy of the page would block it anyway. The result goes into a shadow root, so the styles of the message and those of the page never
 * mix, and links do not navigate.
 */
export function renderEmail(html: string, root: HTMLElement): void {
  DOMPurify.addHook('afterSanitizeAttributes', dropRemote)
  const clean = DOMPurify.sanitize(html, {
    WHOLE_DOCUMENT: true,
    RETURN_DOM: true,
    FORBID_TAGS: ['form', 'input', 'button', 'textarea', 'select', 'iframe', 'frame', 'object', 'embed', 'meta', 'link', 'base'],
    FORBID_ATTR: ['action', 'formaction'],
  }) as HTMLElement
  DOMPurify.removeHook('afterSanitizeAttributes')
  const host = document.createElement('div')
  host.className = 'email'
  const shadow = host.attachShadow({ mode: 'open' })
  const page = document.createElement('div')
  page.style.cssText = 'background:#ffffff;color:#1f2328;font:14px/1.5 system-ui,sans-serif;padding:16px 20px;min-height:100vh;box-sizing:border-box'
  for (const style of Array.from(clean.querySelectorAll('style'))) {
    page.append(style)
  }
  const body = clean.querySelector('body') as HTMLElement
  page.append(...Array.from(body.childNodes))
  shadow.append(page)
  shadow.addEventListener('click', (event) => {
    if ((event.target as Element).closest('a') !== null) {
      event.preventDefault()
    }
  })
  root.append(host)
}
