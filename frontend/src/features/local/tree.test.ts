import { describe, expect, it } from 'vitest'

import { FakeDir, handleOf } from '@/test/fake-local'

import { maxTreeEntries, readDropped, treeFromHandle, treeFromInput, TreeTooLarge } from './tree'

/** A file entry of the browser's directory API. */
function fileEntry(name: string, content = '', fails = false) {
  return {
    isFile: true,
    isDirectory: false,
    name,
    file: (done: (file: File) => void, fail: (error: unknown) => void) => (fails ? fail(new Error('unreadable')) : done(new File([content], name))),
  }
}

/** A directory entry that hands its children out in batches of two, then an empty batch. */
function dirEntry(name: string, children: unknown[], fails = false) {
  return {
    isFile: false,
    isDirectory: true,
    name,
    createReader: () => {
      let next = 0
      return {
        readEntries: (done: (entries: unknown[]) => void, fail: (error: unknown) => void) => {
          if (fails) {
            fail(new Error('unreadable'))
            return
          }
          const batch = children.slice(next, next + 2)
          next += 2
          done(batch)
        },
      }
    },
  }
}

/** What the browser hands to a drop event. */
function dropOf(entries: Array<unknown | null>, files: File[]): DataTransfer {
  return { items: entries.map((entry) => ({ webkitGetAsEntry: () => entry })), files } as unknown as DataTransfer
}

/** A file with a path below the picked folder, as a `webkitdirectory` input reports it. */
function picked(relative: string): File {
  const file = new File(['x'], relative.slice(relative.lastIndexOf('/') + 1))
  Object.defineProperty(file, 'webkitRelativePath', { value: relative })
  return file
}

/**
 * Covers the folder reading of {@link readDropped}, {@link treeFromInput} and {@link treeFromHandle}.
 *
 * Scenarios:
 * - a drop without the directory API, or whose entries are all unknown, carries its files as they are
 * - a drop with folders is read in full, in batches: files, subfolders and empty folders, with paths below the folder, next to the
 *   loose files of the same drop
 * - a folder that cannot be read, or a file that cannot be opened, fails the read
 * - a folder with more entries than the limit fails with TreeTooLarge, in every kind of source
 * - a folder picked with an input becomes a tree with its folders, parents first, and nothing when no file carries a path
 * - a folder of the chosen local folder becomes a tree with its files and subfolders
 */
describe('folder trees', () => {
  it('passes files through when the browser gives no entries', async () => {
    const files = [new File(['a'], 'a.txt')]

    expect((await readDropped({ files } as unknown as DataTransfer)).files).toEqual(files)
    expect((await readDropped(dropOf([null], files))).files).toEqual(files)
    expect((await readDropped({ items: [{}], files } as unknown as DataTransfer)).files).toEqual(files)
  })

  it('reads dropped folders in full next to loose files', async () => {
    const loose = new File(['l'], 'loose.txt')
    const folder = dirEntry('site', [fileEntry('a.txt'), dirEntry('sub', [fileEntry('b.txt'), fileEntry('c.txt'), dirEntry('deep', [])]), dirEntry('empty', [])])

    const dropped = await readDropped(dropOf([folder, fileEntry('loose.txt')], [new File([], 'site'), loose]))

    expect(dropped.files).toEqual([loose])
    expect(dropped.folders).toHaveLength(1)
    expect(dropped.folders[0].name).toBe('site')
    expect(dropped.folders[0].dirs).toEqual(['sub', 'sub/deep', 'empty'])
    expect(dropped.folders[0].files.map((file) => file.path)).toEqual(['a.txt', 'sub/b.txt', 'sub/c.txt'])
  })

  it('fails when a folder or a file cannot be read', async () => {
    await expect(readDropped(dropOf([dirEntry('bad', [], true)], [new File([], 'bad')]))).rejects.toThrow('unreadable')
    await expect(readDropped(dropOf([dirEntry('bad', [fileEntry('x', '', true)])], [new File([], 'bad')]))).rejects.toThrow('unreadable')
  })

  it('refuses a folder with too many entries', async () => {
    const many = Array.from({ length: maxTreeEntries + 1 }, (_, index) => fileEntry(`f${index}`))

    await expect(readDropped(dropOf([dirEntry('big', many)], [new File([], 'big')]))).rejects.toBeInstanceOf(TreeTooLarge)
  })

  it('builds the tree of a folder picked with an input', () => {
    const tree = treeFromInput([picked('site/a.txt'), picked('site/sub/deep/b.txt'), picked('site/sub/c.txt')])

    expect(tree?.name).toBe('site')
    expect(tree?.dirs).toEqual(['sub', 'sub/deep'])
    expect(tree?.files.map((file) => file.path)).toEqual(['a.txt', 'sub/deep/b.txt', 'sub/c.txt'])
    expect(treeFromInput([new File(['x'], 'plain.txt')])).toBeNull()
    expect(treeFromInput([])).toBeNull()
  })

  it('builds the tree of a folder of the chosen local folder', async () => {
    const root = new FakeDir('root').withFile('outside.txt')
    const photos = root.withDir('photos').withFile('a.png', 'A')
    photos.withDir('raw').withFile('b.raw', 'B')

    const tree = await treeFromHandle(handleOf(root), '/photos')

    expect(tree.name).toBe('photos')
    expect(tree.dirs).toEqual(['raw'])
    expect(tree.files.map((file) => file.path).sort()).toEqual(['a.png', 'raw/b.raw'])
    expect(await tree.files.find((file) => file.path === 'a.png')?.file.text()).toBe('A')
  })

  it('refuses a local folder with too many entries', async () => {
    const root = new FakeDir('root')
    const big = root.withDir('big')
    for (let index = 0; index <= maxTreeEntries; index++) {
      big.withFile(`f${index}`)
    }

    await expect(treeFromHandle(handleOf(root), '/big')).rejects.toBeInstanceOf(TreeTooLarge)
  })
})
