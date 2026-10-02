import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { FileTypeIcon } from './FileTypeIcon'

/** The classes of the icon drawn for a kind. */
function classesOf(kind: Parameters<typeof FileTypeIcon>[0]['type']['kind']): string {
  const { container } = render(<FileTypeIcon type={{ kind, name: '' }} />)
  return container.querySelector('svg')?.getAttribute('class') ?? ''
}

/**
 * Covers {@link FileTypeIcon}.
 *
 * Scenarios:
 * - folders are filled with a soft tint of the brand color, so they stand out from files
 * - each group of formats takes its color: backups warn, keys and certificates danger, data info
 * - a kind without an icon of its own gets a plain, muted file
 */
describe('FileTypeIcon', () => {
  it('fills folders and colors the groups of formats', () => {
    expect(classesOf('directory')).toContain('fill-accent-text/20')
    expect(classesOf('backup')).toContain('text-warn')
    expect(classesOf('certificate')).toContain('text-destructive')
    expect(classesOf('data')).toContain('text-info')
  })

  it('draws a plain muted file for a kind without its own icon', () => {
    expect(classesOf('plain')).toContain('text-muted-foreground')
    expect(classesOf('plain')).toContain('lucide-file')
  })
})
