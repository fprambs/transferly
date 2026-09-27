import type { Extension } from '@codemirror/state'
import { languages } from '@codemirror/language-data'
import { LanguageDescription } from '@codemirror/language'

/**
 * Loads the language support for a file, guessed from its name, or `null` when none of the known languages
 * matches it: the file is then edited as plain text.
 */
export async function loadLanguageFor(name: string): Promise<Extension | null> {
  const found = LanguageDescription.matchFilename(languages, name)
  if (found === null) {
    return null
  }
  const support = await found.load()
  return support
}
