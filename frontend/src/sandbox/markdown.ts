import DOMPurify from 'dompurify'
import { marked } from 'marked'

/**
 * Renders Markdown into `root` inside the isolated page (ADR-0018): marked turns it into HTML, and DOMPurify, as marked requires, keeps
 * only plain HTML (no SVG, MathML, styles or forms). Links never navigate, and images from the network do not load under the policy of the
 * page.
 */
export async function renderMarkdown(text: string, root: HTMLElement): Promise<void> {
  const html = await marked.parse(text, { gfm: true })
  root.innerHTML = DOMPurify.sanitize(html, { USE_PROFILES: { html: true }, FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select'] })
  root.classList.add('markdown')
  root.addEventListener('click', (event) => {
    if ((event.target as Element).closest('a') !== null) {
      event.preventDefault()
    }
  })
}
