import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { gzipSync } from 'node:zlib'

/** Production assets are resolved from this script, independent of the caller's directory. */
const dist = resolve(import.meta.dirname, '../frontend/dist')
/** The initial graph includes the entry point and every module preload in the HTML. */
const html = readFileSync(resolve(dist, 'index.html'), 'utf8')
/** Initial script URLs, deduplicated because a script can also be preloaded. */
const urls = new Set([...html.matchAll(/(?:src|href)="(\/assets\/[^\"]+\.js)"/g)].map((match) => match[1]))
/** Compressed byte count across the complete initial graph, not just the main chunk. */
const bytes = [...urls].reduce((total, url) => total + gzipSync(readFileSync(resolve(dist, `.${url}`))).length, 0)
if (urls.size === 0 || bytes > 500_000) {
  throw new Error(`Initial JavaScript exceeds the 500000-byte gzip budget: ${bytes} bytes in ${urls.size} files`)
}
console.log(`Initial JavaScript: ${bytes} gzip bytes in ${urls.size} files (budget: 500000)`)
