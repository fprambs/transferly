import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import { createI18n } from '@/i18n'
import { builtinConnectors, connector } from '@/test/handlers'

import type { ConnectorField } from './schema'
import { useConnectorTexts } from './use-connector-texts'

/** Returns the texts of connectors in a language. */
async function textsIn(language: 'en' | 'es') {
  const i18n = await createI18n(language)
  const wrapper = ({ children }: { children: ReactNode }) => <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
  return renderHook(() => useConnectorTexts(), { wrapper }).result.current
}

/**
 * Covers {@link useConnectorTexts}.
 *
 * Scenarios:
 * - the built-in connectors and their fields are worded in the language of the interface
 * - a connector or a field without translations reads as its own name, a choice as its value, a secret without its own placeholder as kept
 * - each reason is worded with the bounds of its field, and an unknown one as the generic choice message
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
    expect(texts.label('region')).toBe('region')
    expect(texts.field('region')).toEqual({})
    expect(texts.choice('region', 'eu-west')).toBe('eu-west')
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
})
