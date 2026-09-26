import { describe, expect, it } from 'vitest'

import { freeName } from './free-name'

/** Answers that the given paths exist. */
function taken(...paths: string[]): (candidate: string) => Promise<boolean> {
  return (candidate) => Promise.resolve(paths.includes(candidate))
}

/**
 * Covers {@link freeName}.
 *
 * Scenarios:
 * - the number goes before the extension and the first free one is used
 * - names without an extension, with a leading dot or with dots in the folders get the number at the end of the name
 * - nothing is returned when the limit of names is taken
 */
describe('freeName', () => {
  it('numbers the name before its extension', async () => {
    expect(await freeName(taken(), '/d/report.txt')).toBe('/d/report (1).txt')
    expect(await freeName(taken('/d/report (1).txt', '/d/report (2).txt'), '/d/report.txt')).toBe('/d/report (3).txt')
  })

  it('numbers names without an extension at their end', async () => {
    expect(await freeName(taken(), '/d/plain')).toBe('/d/plain (1)')
    expect(await freeName(taken(), '/d/.hidden')).toBe('/d/.hidden (1)')
    expect(await freeName(taken(), '/v1.2/plain')).toBe('/v1.2/plain (1)')
  })

  it('gives up when every name is taken', async () => {
    expect(await freeName(taken('/a (1)', '/a (2)'), '/a', 2)).toBeUndefined()
  })
})
