import { afterEach, describe, expect, it } from 'vitest'

import { FakeDir, handleOf, offerPicker, withdrawPicker } from '@/test/fake-local'

import { freeLocalName, listLocal, LocalNameTaken, localNameExists, makeLocalDirectory, localSupported, openLocalDirectory, pickLocalDirectory, readLocalFile, saveToLocal, UnsafeLocalName } from './local-fs'

/** A stream that delivers text. */
function streamOf(text: string): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(text))
      controller.close()
    },
  })
}

/** A stream that fails after its first chunk. */
function brokenStream(): ReadableStream<Uint8Array> {
  let sent = false
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (sent) {
        controller.error(new Error('connection lost'))
      } else {
        sent = true
        controller.enqueue(new TextEncoder().encode('part'))
      }
    },
  })
}

/**
 * Covers the folder access of {@link listLocal} and its companions.
 *
 * Scenarios:
 * - the picker is detected, and a folder chosen by the user is returned, while a closed dialog gives `null` and other failures are thrown
 * - folders are opened below the root, files are listed with their kind, size and time, and paths are absolute below the root
 * - names are checked against a folder, the first free numbered name is found with or without an extension, and folders are created or reused but never over a file
 * - a file is read by its path, and a missing one fails
 * - a stream is saved as a new file, an existing file or folder of that name is refused and left alone, and a failing stream leaves no partial file
 * - names that are empty, `.`, `..` or hold a separator or NUL, and paths with `..`, are refused before anything is written
 */
describe('local folder access', () => {
  afterEach(() => withdrawPicker())

  it('detects the picker and returns the chosen folder', async () => {
    expect(localSupported()).toBe(false)
    const chosen = new FakeDir('Documents')
    offerPicker(chosen)

    expect(localSupported()).toBe(true)
    expect(await pickLocalDirectory()).toBe(handleOf(chosen))
  })

  it('returns nothing when the dialog is closed and rethrows other failures', async () => {
    offerPicker(null)
    expect(await pickLocalDirectory()).toBeNull()

    Object.defineProperty(window, 'showDirectoryPicker', { configurable: true, value: () => Promise.reject(new DOMException('no', 'SecurityError')) })
    await expect(pickLocalDirectory()).rejects.toThrow('no')
    Object.defineProperty(window, 'showDirectoryPicker', { configurable: true, value: () => Promise.reject(new Error('boom')) })
    await expect(pickLocalDirectory()).rejects.toThrow('boom')
  })

  it('opens folders and lists their entries', async () => {
    const root = new FakeDir('root').withFile('a.txt', 'hello')
    root.withDir('docs').withFile('b.txt', 'x')

    expect(await openLocalDirectory(handleOf(root), "/")).toBe(handleOf(root))
    expect((await openLocalDirectory(handleOf(root), '/docs')).name).toBe('docs')
    await expect(openLocalDirectory(handleOf(root), '/missing')).rejects.toThrow()
    const listing = await listLocal(handleOf(root), '/')
    expect(listing).toEqual([
      expect.objectContaining({ name: 'a.txt', path: '/a.txt', type: 'file', size: 5, modifiedAt: '2026-03-08T07:30:00.000Z' }),
      expect.objectContaining({ name: 'docs', path: '/docs', type: 'directory', size: 0 }),
    ])
    expect((await listLocal(handleOf(root), '/docs'))[0].path).toBe('/docs/b.txt')
  })

  it('reads a file by its path', async () => {
    const root = new FakeDir('root').withFile('a.txt', 'hello')
    root.withDir('docs').withFile('b.txt', 'inside')

    expect(await (await readLocalFile(handleOf(root), '/a.txt')).text()).toBe('hello')
    expect(await (await readLocalFile(handleOf(root), '/docs/b.txt')).text()).toBe('inside')
    await expect(readLocalFile(handleOf(root), '/nothing.txt')).rejects.toThrow()
  })

  it('saves a stream as a new file and never overwrites', async () => {
    const root = new FakeDir('root').withFile('taken.txt', 'mine')
    root.withDir('folder')

    await saveToLocal(handleOf(root), '/', 'new.txt', streamOf('fresh'))
    expect(await (await readLocalFile(handleOf(root), '/new.txt')).text()).toBe('fresh')
    await expect(saveToLocal(handleOf(root), '/', 'taken.txt', streamOf('other'))).rejects.toBeInstanceOf(LocalNameTaken)
    await expect(saveToLocal(handleOf(root), '/', 'folder', streamOf('other'))).rejects.toBeInstanceOf(LocalNameTaken)
    expect(await (await readLocalFile(handleOf(root), '/taken.txt')).text()).toBe('mine')
  })

  it('leaves no partial file when the stream fails', async () => {
    const root = new FakeDir('root')

    await expect(saveToLocal(handleOf(root), '/', 'broken.txt', brokenStream())).rejects.toThrow()

    expect(root.children.has('broken.txt')).toBe(false)
  })

  it('checks names, finds free ones and makes folders', async () => {
    const root = new FakeDir('root').withFile('a.txt').withFile('a (1).txt').withFile('plain').withFile('.hidden')
    root.withDir('docs')

    expect(await localNameExists(handleOf(root), '/', 'a.txt')).toBe(true)
    expect(await localNameExists(handleOf(root), '/', 'docs')).toBe(true)
    expect(await localNameExists(handleOf(root), '/', 'nothing')).toBe(false)
    expect(await freeLocalName(handleOf(root), '/', 'a.txt')).toBe('a (2).txt')
    expect(await freeLocalName(handleOf(root), '/', 'plain')).toBe('plain (1)')
    expect(await freeLocalName(handleOf(root), '/', '.hidden')).toBe('.hidden (1)')

    await makeLocalDirectory(handleOf(root), '/', 'fresh')
    await makeLocalDirectory(handleOf(root), '/', 'docs')
    expect(root.children.has('fresh')).toBe(true)
    await expect(makeLocalDirectory(handleOf(root), '/', 'a.txt')).rejects.toBeInstanceOf(LocalNameTaken)
  })

  it('refuses names and paths that would climb out of the folder', async () => {
    const root = handleOf(new FakeDir('root'))
    for (const name of ['..', '.', '', 'a/b', '..\\evil', 'nul\0']) {
      await expect(saveToLocal(root, '/', name, streamOf('x'))).rejects.toBeInstanceOf(UnsafeLocalName)
      await expect(makeLocalDirectory(root, '/', name)).rejects.toBeInstanceOf(UnsafeLocalName)
    }
    await expect(openLocalDirectory(root, '/a/../..')).rejects.toBeInstanceOf(UnsafeLocalName)
  })

  it('overwrites an existing file only when asked to', async () => {
    const root = new FakeDir('root').withFile('a.txt', 'old')

    await saveToLocal(handleOf(root), '/', 'a.txt', streamOf('new'), true)

    expect(await (await readLocalFile(handleOf(root), '/a.txt')).text()).toBe('new')
  })
})
