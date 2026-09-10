import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  STALE_CHUNK_RELOAD_KEY,
  clearStaleChunkReloadGuard,
  isStaleChunkError,
  reloadOnceForStaleChunk,
} from './staleChunk.ts'

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const data = { ...initial }
  return {
    get length() {
      return Object.keys(data).length
    },
    clear() {
      for (const key of Object.keys(data)) delete data[key]
    },
    getItem(key: string) {
      return Object.prototype.hasOwnProperty.call(data, key) ? data[key] : null
    },
    key() {
      return null
    },
    removeItem(key: string) {
      delete data[key]
    },
    setItem(key: string, value: string) {
      data[key] = value
    },
  }
}

describe('isStaleChunkError', () => {
  it('matches the production dynamic-import MIME failure', () => {
    const err = new TypeError(
      'Failed to fetch dynamically imported module: https://ordryn.com/assets/DashboardView-BHg0u49w.js',
    )
    assert.equal(isStaleChunkError(err), true)
  })

  it('matches other stale-chunk browser messages', () => {
    assert.equal(isStaleChunkError(new Error('error loading dynamically imported module')), true)
    assert.equal(isStaleChunkError(new TypeError('Importing a module script failed.')), true)
    assert.equal(isStaleChunkError(new Error('Unable to preload CSS for /assets/app.css')), true)
  })

  it('ignores unrelated errors', () => {
    assert.equal(isStaleChunkError(new Error('Network Error')), false)
    assert.equal(isStaleChunkError('boom'), false)
  })
})

describe('reloadOnceForStaleChunk', () => {
  it('reloads once then blocks a second attempt', () => {
    const storage = memoryStorage()
    let reloads = 0
    assert.equal(
      reloadOnceForStaleChunk({ storage, reload: () => {
        reloads += 1
      } }),
      true,
    )
    assert.equal(storage.getItem(STALE_CHUNK_RELOAD_KEY), '1')
    assert.equal(
      reloadOnceForStaleChunk({ storage, reload: () => {
        reloads += 1
      } }),
      false,
    )
    assert.equal(reloads, 1)
  })

  it('reloads again after the guard is cleared', () => {
    const storage = memoryStorage({ [STALE_CHUNK_RELOAD_KEY]: '1' })
    let reloads = 0
    clearStaleChunkReloadGuard(storage)
    assert.equal(storage.getItem(STALE_CHUNK_RELOAD_KEY), null)
    assert.equal(
      reloadOnceForStaleChunk({ storage, reload: () => {
        reloads += 1
      } }),
      true,
    )
    assert.equal(reloads, 1)
  })
})
