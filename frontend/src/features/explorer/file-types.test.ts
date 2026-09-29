import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { fileTypeOf } from './file-types'

/** The type of a file with a name. */
function typeOf(name: string, type: RemoteEntry['type'] = 'file') {
  return fileTypeOf({ name, type })
}

/**
 * Covers {@link fileTypeOf} (4.14e).
 *
 * Scenarios:
 * - folders, links and other entries are their own types whatever their name
 * - an extension gives the kind and the format name, case aside, named after the format when it differs from the extension
 * - compressed tarballs count as one format, and a lone .gz is GZIP
 * - files known by their whole name, dotfiles included, take their type, while other dotfiles are plain files unless they have an extension
 * - an unknown extension is named after it, and a file without an extension, or ending in a dot, is a plain file
 */
describe('fileTypeOf', () => {
  it('gives folders, links and other entries their own type', () => {
    expect(typeOf('photos.png', 'directory')).toEqual({ kind: 'directory', name: '' })
    expect(typeOf('latest', 'symlink')).toEqual({ kind: 'symlink', name: '' })
    expect(typeOf('fifo', 'other')).toEqual({ kind: 'other', name: '' })
  })

  it('reads the extension', () => {
    expect(typeOf('logo.PNG')).toEqual({ kind: 'image', name: 'PNG' })
    expect(typeOf('photo.jpg')).toEqual({ kind: 'image', name: 'JPEG' })
    expect(typeOf('deploy.sh')).toEqual({ kind: 'script', name: 'Shell' })
    expect(typeOf('index.min.js')).toEqual({ kind: 'code', name: 'JavaScript' })
    expect(typeOf('report.pdf')).toEqual({ kind: 'document', name: 'PDF' })
    expect(typeOf('server.key')).toEqual({ kind: 'key', name: 'PEM' })
    expect(typeOf('.config.json')).toEqual({ kind: 'data', name: 'JSON' })
  })

  it('treats compressed tarballs as one format', () => {
    expect(typeOf('backup.tar.gz')).toEqual({ kind: 'archive', name: 'TAR.GZ' })
    expect(typeOf('backup.tar.zst')).toEqual({ kind: 'archive', name: 'TAR.ZST' })
    expect(typeOf('dump.sql.gz')).toEqual({ kind: 'archive', name: 'GZIP' })
  })

  it('knows files by their whole name', () => {
    expect(typeOf('Dockerfile')).toEqual({ kind: 'named', name: 'Dockerfile' })
    expect(typeOf('.htaccess')).toEqual({ kind: 'config', name: 'Apache' })
    expect(typeOf('authorized_keys')).toEqual({ kind: 'key', name: 'SSH' })
    expect(typeOf('.vimrc')).toEqual({ kind: 'plain', name: '' })
  })

  it('names unknown extensions and leaves files without one plain', () => {
    expect(typeOf('data.xyz')).toEqual({ kind: 'unknown', name: 'XYZ' })
    expect(typeOf('notes')).toEqual({ kind: 'plain', name: '' })
    expect(typeOf('odd.')).toEqual({ kind: 'plain', name: '' })
  })
})
