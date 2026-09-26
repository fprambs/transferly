import { describe, expect, it } from 'vitest'

import { entryDragType, isEntryDrag, readDrag, writeDrag } from './entry-drag'

/** A minimal drag data store, since jsdom has none. */
function dataTransfer(initial: Record<string, string> = {}): DataTransfer {
  const store = new Map(Object.entries(initial))
  return {
    get types() {
      return [...store.keys()]
    },
    setData: (type: string, value: string) => void store.set(type, value),
    getData: (type: string) => store.get(type) ?? '',
    effectAllowed: 'none',
  } as unknown as DataTransfer
}

/**
 * Covers the drag data of {@link writeDrag}, {@link isEntryDrag} and {@link readDrag}.
 *
 * Scenarios:
 * - a written file is recognized and read back with its workspace, site and path, and the drag is a copy
 * - folders are told from files, and a drag from an older page without the flag reads as a file
 * - a drag without data, without the type, with data that is not JSON, or with the wrong shape reads as nothing
 */
describe('entry drag', () => {
  it('writes a file and reads it back', () => {
    const data = dataTransfer()
    writeDrag(data, { workspaceId: 'w', siteId: 's', path: '/a.txt', isFolder: false })

    expect(isEntryDrag(data)).toBe(true)
    expect(readDrag(data)).toEqual({ workspaceId: 'w', siteId: 's', path: '/a.txt', isFolder: false })
    expect(data.effectAllowed).toBe('copy')
  })

  it('tells folders from files', () => {
    const data = dataTransfer()
    writeDrag(data, { workspaceId: 'w', siteId: 's', path: '/d', isFolder: true })

    expect(readDrag(data)?.isFolder).toBe(true)
    expect(readDrag(dataTransfer({ [entryDragType]: '{"workspaceId":"w","siteId":"s","path":"/x"}' }))?.isFolder).toBe(false)
  })

  it('reads nothing from foreign or malformed drags', () => {
    expect(isEntryDrag(dataTransfer({ Files: '' }))).toBe(false)
    expect(isEntryDrag(null)).toBe(false)
    expect(readDrag(dataTransfer())).toBeNull()
    expect(readDrag(dataTransfer({ [entryDragType]: 'not json' }))).toBeNull()
    expect(readDrag(dataTransfer({ [entryDragType]: 'null' }))).toBeNull()
    expect(readDrag(dataTransfer({ [entryDragType]: '{"siteId":"s","path":"/a"}' }))).toBeNull()
  })
})
