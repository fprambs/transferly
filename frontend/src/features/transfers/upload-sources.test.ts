import { beforeEach, describe, expect, it } from 'vitest'

import { clearUploadSources, forgetUpload, maxSources, recallUpload, rememberUpload } from './upload-sources'

/** Builds a source for a file of the given name. */
function source(name: string) {
  return { file: new File(['x'], name), workspaceId: 'w1', siteId: 's1' }
}

/**
 * Covers the upload sources.
 *
 * Scenarios:
 * - a source is remembered, recalled and forgotten, and all can be cleared
 * - remembering the same transfer again replaces its source and makes it the newest
 * - past the limit the oldest sources are dropped first
 */
describe('upload sources', () => {
  beforeEach(() => {
    clearUploadSources()
  })

  it('remembers, recalls, forgets and clears', () => {
    rememberUpload('t1', source('a'))
    rememberUpload('t2', source('b'))

    expect(recallUpload('t1')?.file.name).toBe('a')
    forgetUpload('t1')
    expect(recallUpload('t1')).toBeUndefined()
    clearUploadSources()
    expect(recallUpload('t2')).toBeUndefined()
  })

  it('replaces a source and makes it the newest', () => {
    for (let index = 0; index < maxSources; index++) {
      rememberUpload(`t${index}`, source(`f${index}`))
    }
    rememberUpload('t0', source('again'))

    rememberUpload('extra', source('extra'))

    expect(recallUpload('t0')?.file.name).toBe('again')
    expect(recallUpload('t1')).toBeUndefined()
  })

  it('drops the oldest sources past the limit', () => {
    for (let index = 0; index < maxSources + 3; index++) {
      rememberUpload(`t${index}`, source(`f${index}`))
    }

    expect(recallUpload('t0')).toBeUndefined()
    expect(recallUpload('t2')).toBeUndefined()
    expect(recallUpload('t3')).toBeDefined()
    expect(recallUpload(`t${maxSources + 2}`)).toBeDefined()
  })
})
