import DOMPurify from 'dompurify'
import { marked } from 'marked'

import { dropRemote } from './remote'

/**
 * Renders Markdown into `root` inside the isolated page (ADR-0018): marked turns it into HTML, and DOMPurify, as marked requires, keeps
 * only plain HTML (no SVG, MathML, styles or forms). Links never navigate, and remote addresses of images are removed, which the policy of
 * the page would block anyway.
 */
export async function renderMarkdown(text: string, root: HTMLElement): Promise<void> {
  const html = await marked.parse(text, { gfm: true })
  DOMPurify.addHook('afterSanitizeAttributes', dropRemote)
  root.innerHTML = DOMPurify.sanitize(html, { USE_PROFILES: { html: true }, FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select'] })
  DOMPurify.removeHook('afterSanitizeAttributes')
  root.classList.add('markdown')
  root.addEventListener('click', (event) => {
    if ((event.target as Element).closest('a') !== null) {
      event.preventDefault()
    }
  })
}
