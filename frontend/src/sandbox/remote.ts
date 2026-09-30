/** Attributes through which HTML loads something, whose remote addresses are removed. */
const loading = ['src', 'srcset', 'background', 'poster', 'data']

/** A remote address, as an attribute value or inside a list of them. */
const remote = /^\s*(https?:)?\/\//i

/** A remote address inside CSS. */
const remoteCss = /url\(\s*(['"]?)\s*(https?:)?\/\/[^)]*\)/gi

/**
 * Removes what would load from the network from an element cleaned by DOMPurify: remote addresses of attributes and of CSS, inline or in
 * style elements. The policy of the isolated page blocks them anyway; removing them keeps them from being asked for at all, also where no
 * policy applies, as in development.
 */
export function dropRemote(node: Element): void {
  for (const name of loading) {
    const value = node.getAttribute(name)
    if (value !== null && (remote.test(value) || value.split(',').some((part) => remote.test(part)))) {
      node.removeAttribute(name)
    }
  }
  const style = node.getAttribute('style')
  if (style !== null) {
    node.setAttribute('style', style.replace(remoteCss, 'none'))
  }
  if (node.tagName === 'STYLE') {
    node.textContent = (node.textContent as string).replace(remoteCss, 'none')
  }
}
