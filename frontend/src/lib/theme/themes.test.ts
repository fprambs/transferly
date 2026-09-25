/// <reference types="node" />
import { readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import { contrastRatio } from './contrast'
import { defaultThemeId, findTheme, systemLightThemeId, themes } from './themes'
import { type ThemeTokens, toCssVariables } from './tokens'

/** Pairs of colors that must be readable, with the least contrast each needs (WCAG 2.1 AA for text is 4.5). */
const readablePairs: ReadonlyArray<{ text: keyof ThemeTokens; on: ReadonlyArray<keyof ThemeTokens>; minimum: number }> = [
  { text: 'text', on: ['bg', 'surface', 'raised'], minimum: 7 },
  { text: 'muted', on: ['bg', 'surface', 'raised'], minimum: 4.5 },
  { text: 'faint', on: ['bg', 'surface', 'raised'], minimum: 4.5 },
  { text: 'accentText', on: ['bg', 'surface', 'raised'], minimum: 4.5 },
  { text: 'ink', on: ['accent'], minimum: 4.5 },
  { text: 'ok', on: ['bg', 'surface'], minimum: 4.5 },
  { text: 'danger', on: ['bg', 'surface'], minimum: 4.5 },
  { text: 'warn', on: ['bg', 'surface'], minimum: 4.5 },
  { text: 'info', on: ['bg', 'surface'], minimum: 4.5 },
]

/**
 * Covers the registry of {@link themes}.
 *
 * Scenarios:
 * - the list has twelve themes with unique identifiers the server accepts and non-empty names
 * - the default and the system light theme exist, and the default is the first one
 * - every color of every theme is a six-digit hexadecimal color
 * - a dark theme is darker than its own text and a light theme lighter, so the scheme label is truthful
 */
describe('themes', () => {
  it('lists twelve themes with valid identifiers and names', () => {
    expect(themes).toHaveLength(12)
    expect(new Set(themes.map((theme) => theme.id)).size).toBe(12)
    for (const theme of themes) {
      expect(theme.id).toMatch(/^[a-z0-9][a-z0-9-]{0,39}$/)
      expect(theme.name.length).toBeGreaterThan(0)
    }
  })

  it('has the default and the system light theme', () => {
    expect(themes[0]?.id).toBe(defaultThemeId)
    expect(findTheme(defaultThemeId)?.scheme).toBe('dark')
    expect(findTheme(systemLightThemeId)?.scheme).toBe('light')
  })

  it('uses six-digit hexadecimal colors everywhere', () => {
    for (const theme of themes) {
      for (const [name, value] of Object.entries(theme.tokens)) {
        expect(value, `${theme.id}.${name}`).toMatch(/^#[0-9a-f]{6}$/)
      }
    }
  })

  it('labels the scheme truthfully', () => {
    for (const theme of themes) {
      const backgroundIsDarker = contrastRatio(theme.tokens.bg, '#000000') < contrastRatio(theme.tokens.bg, '#ffffff')
      expect(backgroundIsDarker, theme.id).toBe(theme.scheme === 'dark')
    }
  })
})

/**
 * Covers the readability of every theme.
 *
 * Scenarios:
 * - primary, secondary and tertiary text, the brand color as text, text on the brand color and the four state
 *   colors meet their least contrast on the backgrounds they are drawn on, in every theme
 */
describe('theme readability', () => {
  for (const theme of themes) {
    it(`${theme.name} is readable`, () => {
      const failures: string[] = []
      for (const { text, on, minimum } of readablePairs) {
        for (const background of on) {
          const ratio = contrastRatio(theme.tokens[text], theme.tokens[background])
          if (ratio < minimum) {
            failures.push(`${text} on ${background}: ${ratio.toFixed(2)} < ${minimum}`)
          }
        }
      }
      expect(failures).toEqual([])
    })
  }
})

/**
 * Covers {@link toCssVariables}.
 *
 * Scenarios:
 * - the shadcn names map to the design tokens, and the extra steps are exposed
 * - the soft accent is a mix of the accent with transparency
 * - the values of the default theme are exactly the ones written in the :root block of index.css, so the page paints
 *   with the right colors before any script runs
 */
describe('toCssVariables', () => {
  const tokens = findTheme(defaultThemeId)!.tokens
  const variables = toCssVariables(tokens)

  it('maps shadcn names and the extra steps', () => {
    expect(variables['--background']).toBe(tokens.bg)
    expect(variables['--foreground']).toBe(tokens.text)
    expect(variables['--primary']).toBe(tokens.accent)
    expect(variables['--primary-foreground']).toBe(tokens.ink)
    expect(variables['--muted-foreground']).toBe(tokens.muted)
    expect(variables['--destructive']).toBe(tokens.danger)
    expect(variables['--border']).toBe(tokens.line)
    expect(variables['--faint']).toBe(tokens.faint)
    expect(variables['--accent-text']).toBe(tokens.accentText)
    expect(variables['--ok']).toBe(tokens.ok)
    expect(variables['--warn']).toBe(tokens.warn)
    expect(variables['--info']).toBe(tokens.info)
  })

  it('mixes the soft accent from the accent', () => {
    expect(variables['--accent-soft']).toBe(`color-mix(in srgb, ${tokens.accent} 14%, transparent)`)
  })

  it('matches the :root block of index.css', () => {
    const stylesheet = readFileSync(path.resolve(import.meta.dirname, '../../index.css'), 'utf8')
    const root = /:root\s*\{([^}]*)\}/.exec(stylesheet)?.[1] ?? ''
    const declared = Object.fromEntries(
      [...root.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((match) => [match[1], (match[2] as string).trim()]),
    )

    for (const [name, value] of Object.entries(variables)) {
      expect(declared[name], name).toBe(value)
    }
  })
})
