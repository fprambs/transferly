/** The methods of the WebAuthn JSON API that browsers added in 2024 and 2025, which the global types do not declare yet. */
interface JsonApi {
  /** Turns the JSON options of a registration into the object `navigator.credentials.create` takes. */
  parseCreationOptionsFromJSON: (options: object) => CredentialCreationOptions['publicKey']
  /** Turns the JSON options of a sign-in into the object `navigator.credentials.get` takes. */
  parseRequestOptionsFromJSON: (options: object) => CredentialRequestOptions['publicKey']
}

/** What a browser gives back: a credential that can describe itself as JSON. */
interface JsonCredential {
  /** The credential as the JSON a server expects. */
  toJSON: () => object
}

/** Reads the constructor of public key credentials, or `undefined` when the browser has none. */
function api(): (JsonApi & typeof PublicKeyCredential) | undefined {
  return (globalThis as { PublicKeyCredential?: JsonApi & typeof PublicKeyCredential }).PublicKeyCredential
}

/** Tells whether the browser can make and use passkeys: it has WebAuthn and the JSON helpers, and the page is a secure context. */
export function passkeysSupported(): boolean {
  const constructor = api()
  return constructor !== undefined && typeof constructor.parseCreationOptionsFromJSON === 'function' && typeof navigator.credentials?.create === 'function'
}

/** Asks the browser to make a passkey from the options of a server, and returns the credential as JSON. It rejects when the user cancels. */
export async function createPasskey(options: object): Promise<object> {
  const constructor = api() as JsonApi
  const credential = (await navigator.credentials.create({ publicKey: constructor.parseCreationOptionsFromJSON(options) })) as unknown as JsonCredential
  return credential.toJSON()
}

/** Asks the browser to sign in with a passkey from the options of a server, and returns the credential as JSON. It rejects when the user cancels. */
export async function getPasskey(options: object): Promise<object> {
  const constructor = api() as JsonApi
  const credential = (await navigator.credentials.get({ publicKey: constructor.parseRequestOptionsFromJSON(options) })) as unknown as JsonCredential
  return credential.toJSON()
}
