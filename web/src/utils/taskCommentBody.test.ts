import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  extractTaskRefIDs,
  extractTaskRefQueries,
  hasImageMarkdown,
  insertMarkdownAtCursor,
  insertTaskRef,
  isInsertedTaskRef,
  isSafeImageSrc,
  previewWithoutImages,
} from './taskCommentBody.ts'

describe('isSafeImageSrc', () => {
  it('allows https and local uploads', () => {
    assert.equal(isSafeImageSrc('https://cdn.example.com/a.png'), true)
    assert.equal(isSafeImageSrc('/uploads/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.png'), true)
  })
  it('rejects javascript and relative tricks', () => {
    assert.equal(isSafeImageSrc('javascript:alert(1)'), false)
    assert.equal(isSafeImageSrc('data:image/png;base64,aaa'), false)
    assert.equal(isSafeImageSrc('/uploads/../secret.png'), false)
    assert.equal(isSafeImageSrc('//evil.example/x.png'), false)
  })
})

describe('previewWithoutImages', () => {
  it('replaces markdown with a short label', () => {
    assert.equal(
      previewWithoutImages('Before ![logo](https://cdn.example.com/a.png) after'),
      'Before [image: logo] after',
    )
  })
})

describe('hasImageMarkdown', () => {
  it('detects a safe image', () => {
    assert.equal(hasImageMarkdown('![a](https://cdn.example.com/a.png)'), true)
    assert.equal(hasImageMarkdown('no pictures here'), false)
  })
})

describe('insertMarkdownAtCursor', () => {
  it('puts the image on its own line', () => {
    const got = insertMarkdownAtCursor('hello', '![cat](https://cdn.example.com/c.png)', 5)
    assert.equal(got.body, 'hello\n![cat](https://cdn.example.com/c.png)')
  })
})

describe('extractTaskRefIDs', () => {
  it('extracts numeric task references', () => {
    const ids = extractTaskRefIDs('See #12 and [[34]] and #12 again')
    assert.deepEqual(ids, [12, 34])
  })

  it('ignores non-numeric hashtags', () => {
    const ids = extractTaskRefIDs('Look at #new-task and #frontend')
    assert.deepEqual(ids, [])
  })
})

describe('extractTaskRefQueries', () => {
  it('extracts non-numeric task name queries', () => {
    const queries = extractTaskRefQueries('Check #new and #my-task and #New')
    assert.deepEqual(queries, ['new', 'my-task'])
  })

  it('ignores purely numeric hashtags', () => {
    const queries = extractTaskRefQueries('Check #123 and [[456]]')
    assert.deepEqual(queries, [])
  })
})

describe('isInsertedTaskRef', () => {
  it('returns true only when bracket syntax is present', () => {
    assert.equal(isInsertedTaskRef('Check [[190]] now', 190), true)
    assert.equal(isInsertedTaskRef('Check #190 now', 190), false)
    assert.equal(isInsertedTaskRef('Check 190 now', 190), false)
    assert.equal(isInsertedTaskRef('Check #new now', 190), false)
  })
})

describe('insertTaskRef', () => {
  it('replaces numeric hash with brackets', () => {
    assert.equal(insertTaskRef('#2', 2), '[[2]]')
    assert.equal(insertTaskRef('Fix #2 now', 2), 'Fix [[2]] now')
  })

  it('replaces named query hashtag with task ID brackets', () => {
    assert.equal(insertTaskRef('#new', 190, 'new'), '[[190]]')
    assert.equal(insertTaskRef('working on #new today', 190, 'new'), 'working on [[190]] today')
    assert.equal(insertTaskRef('Check #my-task!', 190, 'my-task'), 'Check [[190]]!')
  })

  it('replaces named query case-insensitively', () => {
    assert.equal(insertTaskRef('Check #New now', 190, 'new'), 'Check [[190]] now')
    assert.equal(insertTaskRef('Check #new now', 190, 'New'), 'Check [[190]] now')
  })

  it('does not double insert if already bracketed', () => {
    assert.equal(insertTaskRef('See [[190]] please', 190, 'new'), 'See [[190]] please')
  })

  it('falls back to appending when no matching hashtag exists in body', () => {
    assert.equal(insertTaskRef('Some text', 190), 'Some text [[190]]')
    assert.equal(insertTaskRef('Some text', 190, 'unmatched'), 'Some text [[190]]')
    assert.equal(insertTaskRef('', 190), '[[190]]')
  })
})

