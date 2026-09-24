import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import {
  findCommentsInsideBodies,
  findUndocumentedDeclarations,
  type Violation,
} from './docs-rules.ts'

const roots = ['src', 'guards', 'e2e'].map((directory) => path.resolve(import.meta.dirname, '..', directory))

/** Lists the TypeScript sources under a directory, skipping declaration files. */
function listSources(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      return listSources(full)
    }
    return /\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts') ? [full] : []
  })
}

/** Runs a rule over every source file and formats the violations for display. */
function check(rule: (file: string, text: string) => Violation[]): string[] {
  return roots.flatMap(listSources).flatMap((file) => {
    const relative = path.relative(path.resolve(import.meta.dirname, '..'), file)
    return rule(relative, readFileSync(file, 'utf8')).map(
      (violation) => `${violation.file}:${violation.line}: ${violation.message}`,
    )
  })
}

/**
 * Repository-wide documentation guards for the frontend sources.
 *
 * Scenarios:
 * - no source file contains a comment inside a function body
 * - every declaration, class or interface member and test suite has a TSDoc comment
 */
describe('frontend documentation', () => {
  it('has no comments inside function bodies', () => {
    expect(check(findCommentsInsideBodies)).toEqual([])
  })

  it('documents every declaration', () => {
    expect(check(findUndocumentedDeclarations)).toEqual([])
  })
})
