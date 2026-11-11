/**
 * Passkey (WebAuthn) helpers. The server sends options with base64url strings
 * where the browser API wants ArrayBuffers, and expects responses the same way.
 */

export function b64urlToBuffer(s: string): ArrayBuffer {
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const bin = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out.buffer
}

export function bufferToB64url(buf: ArrayBuffer | ArrayBufferView): string {
  const bytes =
    buf instanceof ArrayBuffer ? new Uint8Array(buf) : new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

type Descriptor = { id: string; type: string; transports?: string[] }
type ServerCreationOptions = {
  publicKey: Record<string, unknown> & {
    challenge: string
    user: { id: string; name: string; displayName: string }
    excludeCredentials?: Descriptor[]
  }
}
type ServerRequestOptions = {
  publicKey: Record<string, unknown> & { challenge: string; allowCredentials?: Descriptor[] }
}

function descriptors(list?: Descriptor[]) {
  return list?.map((d) => ({ ...d, id: b64urlToBuffer(d.id) })) as PublicKeyCredentialDescriptor[] | undefined
}

/** Server registration options → navigator.credentials.create() options. */
export function toCreationOptions(o: ServerCreationOptions): CredentialCreationOptions {
  const pk = o.publicKey
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialCreationOptions),
      challenge: b64urlToBuffer(pk.challenge),
      user: { ...pk.user, id: b64urlToBuffer(pk.user.id) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    },
  }
}

/** Server sign-in options → navigator.credentials.get() options. */
export function toRequestOptions(o: ServerRequestOptions): CredentialRequestOptions {
  const pk = o.publicKey
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialRequestOptions),
      challenge: b64urlToBuffer(pk.challenge),
      allowCredentials: descriptors(pk.allowCredentials),
    },
  }
}

/** A new credential as JSON for the server. */
export function registrationToJSON(cred: PublicKeyCredential) {
  const r = cred.response as AuthenticatorAttestationResponse
  return {
    id: cred.id,
    rawId: bufferToB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: bufferToB64url(r.clientDataJSON),
      attestationObject: bufferToB64url(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : undefined,
    },
  }
}

/** A sign-in assertion as JSON for the server. */
export function assertionToJSON(cred: PublicKeyCredential) {
  const r = cred.response as AuthenticatorAssertionResponse
  return {
    id: cred.id,
    rawId: bufferToB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: bufferToB64url(r.clientDataJSON),
      authenticatorData: bufferToB64url(r.authenticatorData),
      signature: bufferToB64url(r.signature),
      userHandle: r.userHandle ? bufferToB64url(r.userHandle) : undefined,
    },
  }
}

/** Whether this browser can use passkeys at all. */
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && typeof window.PublicKeyCredential === 'function'
}

/** True when the person closed or cancelled the passkey prompt. */
export function isPasskeyCancel(err: unknown): boolean {
  return err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'AbortError')
}
