import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import { createI18n } from '@/i18n'
import { builtinConnectors, connector } from '@/test/handlers'

import type { ConnectorField } from './schema'
import { useConnectorTexts } from './use-connector-texts'

/** Returns the texts of connectors in a language, those of one connector when its type is given. */
async function textsIn(language: 'en' | 'es', type?: string) {
  const i18n = await createI18n(language)
  const wrapper = ({ children }: { children: ReactNode }) => <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
  return renderHook(() => useConnectorTexts(type), { wrapper }).result.current
}

/**
 * Covers {@link useConnectorTexts}.
 *
 * Scenarios:
 * - the built-in connectors and their fields are worded in the language of the interface
 * - a connector or a field without translations reads as its own name, a choice as its value, a secret without its own placeholder as kept
 * - each reason is worded with the bounds of its field, and an unknown one as the generic choice message
 * - a connector words its own fields over the shared texts, keeping the shared choices it does not word, and a connector without texts of
 *   its own reads the shared ones
 */
describe('useConnectorTexts', () => {
  it('words the built-in connectors in the language of the interface', async () => {
    const texts = await textsIn('es')
    const [ftp] = builtinConnectors

    expect(texts.connector(ftp)).toBe('FTP - Protocolo de Transferencia de Archivos')
    expect(texts.label('host')).toBe('Servidor')
  })

  it('falls back to names and values without translations', async () => {
    const texts = await textsIn('en')

    expect(texts.connectorLabel).toBe('Protocol')
    expect(texts.connector(connector('bucket', 'Bucket'))).toBe('Bucket')
    expect(texts.label('shelf')).toBe('shelf')
    expect(texts.field('shelf')).toEqual({ choices: {}, notes: {}, warnings: {} })
    expect(texts.choice('shelf', 'top')).toBe('top')
    expect(texts.choice('auth', 'password')).toBe('Normal')
    expect(texts.keep('token')).toBe('Leave blank to keep the stored one.')
    expect(texts.keep('password')).toBe('Leave blank to keep the existing password.')
    expect(texts.group('proxy')).toBe('Proxy')
    expect(texts.group('connection')).toBeUndefined()
  })

  it('words each reason with the bounds of its field', async () => {
    const texts = await textsIn('en')
    const field = { name: 'note', kind: 'text', group: 'advanced', required: false, min: 1, max: 10, maxLength: 3 } as ConnectorField

    expect(texts.reason(field, 'length')).toBe('Use at most 3 characters.')
    expect(texts.reason(field, 'range')).toBe('Enter a number between 1 and 10.')
    expect(texts.reason(undefined, 'required')).toBe('Fill in this field.')
    expect(texts.reason(field, 'gibberish')).toBe('Choose one of the options.')
  })

  it('words the fields of a connector over the shared texts', async () => {
    const s3 = await textsIn('en', 's3')
    expect(s3.label('host')).toBe('Endpoint')
    expect(s3.field('host').hint).toMatch(/s3.amazonaws.com/)
    expect(s3.choice('auth', 'access_key')).toBe('Access key')
    expect(s3.field('auth').notes?.anonymous).toMatch(/public bucket/)
    expect(s3.field('auth').notes?.ask).toMatch(/No password is saved/)
    expect(s3.field('tls').warnings?.false).toMatch(/clear text/)

    const sftp = await textsIn('en', 'sftp')
    expect(sftp.label('host')).toBe('Host')
    expect(sftp.field('auth').notes?.anonymous).toMatch(/Signs in as anonymous/)
  })
})
