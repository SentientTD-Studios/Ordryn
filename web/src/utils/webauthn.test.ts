import { test } from 'node:test'
import assert from 'node:assert/strict'
import { b64urlToBuffer, bufferToB64url, toCreationOptions, toRequestOptions } from './webauthn.ts'

test('base64url round-trips every byte value and padding length', () => {
  for (let len = 0; len < 70; len++) {
    const bytes = new Uint8Array(len).map((_, i) => (i * 37 + len) & 255)
    const s = bufferToB64url(bytes)
    assert.doesNotMatch(s, /[+/=]/)
    assert.deepEqual(new Uint8Array(b64urlToBuffer(s)), bytes)
  }
})

test('options conversion decodes challenge, user id, and credential ids', () => {
  const create = toCreationOptions({
    publicKey: {
      challenge: 'AQID',
      user: { id: 'BAUG', name: 'a@b.c', displayName: 'A' },
      excludeCredentials: [{ id: 'BwgJ', type: 'public-key' }],
      rp: { id: 'x.test', name: 'X' },
    },
  })
  assert.deepEqual(new Uint8Array(create.publicKey!.challenge as ArrayBuffer), new Uint8Array([1, 2, 3]))
  assert.deepEqual(new Uint8Array(create.publicKey!.user.id as ArrayBuffer), new Uint8Array([4, 5, 6]))
  assert.deepEqual(new Uint8Array(create.publicKey!.excludeCredentials![0].id as ArrayBuffer), new Uint8Array([7, 8, 9]))
  assert.equal(create.publicKey!.rp.id, 'x.test')

  const get = toRequestOptions({ publicKey: { challenge: 'AQID', rpId: 'x.test' } })
  assert.deepEqual(new Uint8Array(get.publicKey!.challenge as ArrayBuffer), new Uint8Array([1, 2, 3]))
  assert.equal(get.publicKey!.allowCredentials, undefined)
})
