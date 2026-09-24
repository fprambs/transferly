import { describe, expect, it } from 'vitest'

import { findCommentsInsideBodies, findUndocumentedDeclarations } from './docs-rules.ts'

/** Joins source lines into one source text. */
function source(...lines: string[]): string {
  return lines.join('\n')
}

/**
 * Covers {@link findCommentsInsideBodies}.
 *
 * Scenarios:
 * - documentation blocks and comments outside bodies are allowed
 * - line, block and trailing comments inside functions, methods and arrow functions are reported
 * - comments inside JSX expressions of a component are reported
 * - tool directives are allowed inside bodies
 * - the reported line points at the comment
 */
describe('findCommentsInsideBodies', () => {
  it('allows documentation blocks and comments outside function bodies', () => {
    const text = source('// header', '/** Adds one. */', 'export function inc(n: number) {', '  return n + 1', '}')

    expect(findCommentsInsideBodies('a.ts', text)).toEqual([])
  })

  it.each([
    ['a line comment in a function', source('function f() {', '  // note', '  return 1', '}')],
    ['a block comment in an arrow function', source('const f = () => {', '  /* note */', '  return 1', '}')],
    ['a trailing comment', source('function f() {', '  return 1 // note', '}')],
    ['a comment in a method', source('class A {', '  m() {', '    // note', '  }', '}')],
    ['a comment in an expression-bodied arrow', source('const f = () => /* note */ 1')],
    ['a documentation block inside a body', source('function f() {', '  /** note */', '  return 1', '}')],
  ])('reports %s', (_name, text) => {
    expect(findCommentsInsideBodies('a.ts', text)).toHaveLength(1)
  })

  it('reports comments inside the JSX of a component', () => {
    const text = source('function C() {', '  return <p>{/* note */}</p>', '}')

    expect(findCommentsInsideBodies('c.tsx', text)).toHaveLength(1)
  })

  it('allows tool directives inside bodies', () => {
    const text = source('function f(x: number) {', '  // @ts-expect-error intentional', '  return x.y', '}')

    expect(findCommentsInsideBodies('a.ts', text)).toEqual([])
  })

  it('reports the line of the comment', () => {
    const text = source('function f() {', '  const a = 1', '  // note', '  return a', '}')

    expect(findCommentsInsideBodies('a.ts', text)).toEqual([
      { file: 'a.ts', line: 3, message: 'comment inside a function body' },
    ])
  })
})

/**
 * Covers {@link findUndocumentedDeclarations}.
 *
 * Scenarios:
 * - a fully documented module has no violations
 * - functions, classes, interfaces, type aliases and enums without TSDoc are reported
 * - class members and interface members without TSDoc are reported
 * - exported constants and constants holding functions need TSDoc; plain private constants do not
 * - top-level `describe` suites in test files need TSDoc; other files are not affected
 */
describe('findUndocumentedDeclarations', () => {
  it('accepts a fully documented module', () => {
    const text = source(
      '/** Adds one. */',
      'export function inc(n: number) { return n + 1 }',
      '/** A point. */',
      'export interface Point {',
      '  /** Horizontal position. */',
      '  x: number',
      '}',
    )

    expect(findUndocumentedDeclarations('a.ts', text)).toEqual([])
  })

  it.each([
    ['a function', 'function f() {}'],
    ['a class', 'class A {}'],
    ['an interface', 'interface I {}'],
    ['a type alias', 'type T = string'],
    ['an enum', 'enum E { A }'],
    ['an exported constant', 'export const limit = 5'],
    ['a constant holding a function', 'const f = () => 1'],
  ])('reports %s without TSDoc', (_name, text) => {
    expect(findUndocumentedDeclarations('a.ts', text)).toHaveLength(1)
  })

  it('does not require TSDoc on plain private constants', () => {
    expect(findUndocumentedDeclarations('a.ts', 'const limit = 5')).toEqual([])
  })

  it('reports undocumented class and interface members', () => {
    const text = source(
      '/** A. */',
      'class A {',
      '  value = 1',
      '  run() {}',
      '}',
      '/** I. */',
      'interface I {',
      '  name: string',
      '}',
    )

    expect(findUndocumentedDeclarations('a.ts', text)).toHaveLength(3)
  })

  it('requires TSDoc on top-level describe suites in test files only', () => {
    const text = "describe('suite', () => {})"

    expect(findUndocumentedDeclarations('a.test.ts', text)).toHaveLength(1)
    expect(findUndocumentedDeclarations('a.ts', text)).toEqual([])
    expect(findUndocumentedDeclarations('a.test.ts', source('/** Covers a. */', text))).toEqual([])
  })
})
