/**
 * WebAuthn encoding utilities and ceremony wrappers.
 *
 * The go-webauthn server expects base64url-encoded binary fields in the
 * standard PublicKeyCredential JSON format.
 */

// ---------------------------------------------------------------------------
// Base64url encoding/decoding
// ---------------------------------------------------------------------------

/** ArrayBuffer → base64url string (no padding) */
export function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer)
  let str = ''
  for (const b of bytes) str += String.fromCharCode(b)
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

/** base64url string → ArrayBuffer */
export function base64urlToBuffer(str) {
  const base64 = str.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64.padEnd(base64.length + (4 - (base64.length % 4)) % 4, '=')
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer
}

// ---------------------------------------------------------------------------
// Options preparation — decode base64url fields from server response
// ---------------------------------------------------------------------------

/**
 * Prepare PublicKeyCredentialCreationOptions from server response.
 * Decodes challenge, user.id, and any excludeCredentials ids.
 */
export function prepareCreationOptions(serverOptions) {
  const opts = { ...serverOptions }
  opts.challenge = base64urlToBuffer(opts.challenge)
  if (opts.user) {
    opts.user = { ...opts.user, id: base64urlToBuffer(opts.user.id) }
  }
  if (opts.excludeCredentials) {
    opts.excludeCredentials = opts.excludeCredentials.map(c => ({
      ...c,
      id: base64urlToBuffer(c.id),
    }))
  }
  return opts
}

/**
 * Prepare PublicKeyCredentialRequestOptions from server response.
 * Decodes challenge and any allowCredentials ids.
 */
export function prepareRequestOptions(serverOptions) {
  const opts = { ...serverOptions }
  opts.challenge = base64urlToBuffer(opts.challenge)
  if (opts.allowCredentials) {
    opts.allowCredentials = opts.allowCredentials.map(c => ({
      ...c,
      id: base64urlToBuffer(c.id),
    }))
  }
  return opts
}

// ---------------------------------------------------------------------------
// Credential serialisation — convert browser response to server-expected JSON
// ---------------------------------------------------------------------------

/**
 * Serialize a PublicKeyCredential creation response for POST register/finish.
 * Prefers credential.toJSON() (modern browsers), falls back to manual encoding.
 */
export function serializeCreationResponse(credential) {
  if (typeof credential.toJSON === 'function') {
    return credential.toJSON()
  }
  // Manual fallback
  const r = credential.response
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(r.clientDataJSON),
      attestationObject: bufferToBase64url(r.attestationObject),
    },
  }
}

/**
 * Serialize a PublicKeyCredential assertion response for POST login/finish.
 * Prefers credential.toJSON() (modern browsers), falls back to manual encoding.
 */
export function serializeAssertionResponse(credential) {
  if (typeof credential.toJSON === 'function') {
    return credential.toJSON()
  }
  // Manual fallback
  const r = credential.response
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(r.clientDataJSON),
      authenticatorData: bufferToBase64url(r.authenticatorData),
      signature: bufferToBase64url(r.signature),
      userHandle: r.userHandle ? bufferToBase64url(r.userHandle) : null,
    },
  }
}

