import { afterEach, describe, expect, it, vi } from 'vitest'

import { createPasskey, getPasskey, passkeysSupported } from './webauthn'

/** Installs a fake browser: the constructor with its JSON helpers and the credentials container. */
function install(): { create: ReturnType<typeof vi.fn>; get: ReturnType<typeof vi.fn> } {
  const create = vi.fn(async () => ({ toJSON: () => ({ id: 'made' }) }))
  const get = vi.fn(async () => ({ toJSON: () => ({ id: 'used' }) }))
  vi.stubGlobal('PublicKeyCredential', {
    parseCreationOptionsFromJSON: (options: object) => ({ parsed: 'creation', ...options }),
    parseRequestOptionsFromJSON: (options: object) => ({ parsed: 'request', ...options }),
  })
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { create, get } })
  return { create, get }
}

/**
 * Covers the WebAuthn helpers.
 *
 * Scenarios:
 * - a browser without WebAuthn, without the JSON helpers or without a credentials container is reported as unsupported, and one with all of them as supported
 * - creating and using a passkey passes the parsed options to the browser and returns the credential as JSON
 */
describe('webauthn', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Object.defineProperty(navigator, 'credentials', { configurable: true, value: undefined })
  })

  it('tells whether passkeys can be used', () => {
    expect(passkeysSupported()).toBe(false)
    vi.stubGlobal('PublicKeyCredential', {})
    expect(passkeysSupported()).toBe(false)
    install()
    expect(passkeysSupported()).toBe(true)
    Object.defineProperty(navigator, 'credentials', { configurable: true, value: undefined })
    expect(passkeysSupported()).toBe(false)
  })

  it('creates and uses a passkey', async () => {
    const browser = install()

    expect(await createPasskey({ challenge: 'c' })).toEqual({ id: 'made' })
    expect(browser.create).toHaveBeenCalledWith({ publicKey: { parsed: 'creation', challenge: 'c' } })
    expect(await getPasskey({ challenge: 'd' })).toEqual({ id: 'used' })
    expect(browser.get).toHaveBeenCalledWith({ publicKey: { parsed: 'request', challenge: 'd' } })
  })
})
