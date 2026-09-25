import { describe, expect, it, vi } from 'vitest'

import { startDownload } from './download'

/**
 * Covers {@link startDownload}.
 *
 * Scenarios:
 * - a temporary link to the URL is clicked and removed again
 */
describe('startDownload', () => {
  it('clicks a temporary link and removes it', () => {
    const clicks: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicks.push(this.href)
      expect(document.body.contains(this)).toBe(true)
    })

    startDownload('http://localhost/api/v1/files/content?path=%2Fa.txt')

    expect(clicks).toEqual(['http://localhost/api/v1/files/content?path=%2Fa.txt'])
    expect(document.querySelector('a[rel="noopener"]')).toBeNull()
  })
})
