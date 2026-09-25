/**
 * Starts a browser download of a URL by clicking a temporary link.
 *
 * The server answers with `Content-Disposition: attachment`, so the page stays where it is.
 */
export function startDownload(url: string): void {
  const link = document.createElement('a')
  link.href = url
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
}
