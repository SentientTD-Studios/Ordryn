import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { formatAutoSprintPreview, parseOptionalNumberInput } from './autoSprintForm.ts'

describe('parseOptionalNumberInput', () => {
  it('treats empty values as unset', () => {
    assert.equal(parseOptionalNumberInput(''), null)
    assert.equal(parseOptionalNumberInput('   '), null)
    assert.equal(parseOptionalNumberInput(null), null)
    assert.equal(parseOptionalNumberInput(undefined), null)
  })

  it('accepts numbers from Vue number inputs without calling string methods', () => {
    assert.equal(parseOptionalNumberInput(30), 30)
    assert.equal(parseOptionalNumberInput(0), 0)
    assert.equal(parseOptionalNumberInput(7), 7)
  })

  it('parses trimmed numeric strings', () => {
    assert.equal(parseOptionalNumberInput('30'), 30)
    assert.equal(parseOptionalNumberInput(' 7 '), 7)
  })

  it('keeps non-numeric strings as NaN so callers can reject them', () => {
    assert.equal(Number.isNaN(parseOptionalNumberInput('abc')), true)
  })
})

describe('formatAutoSprintPreview', () => {
  it('returns empty until length is a positive integer', () => {
    assert.equal(formatAutoSprintPreview('', 7), '')
    assert.equal(formatAutoSprintPreview(0, 7), '')
  })

  it('describes a follow-up sprint when only length is set', () => {
    assert.equal(
      formatAutoSprintPreview(30, ''),
      'Example: a sprint ending 2026-08-31 is followed by 2026-09-01 – 2026-09-30 with no lock date.',
    )
  })

  it('does not throw when Vue emits both fields as numbers', () => {
    assert.equal(
      formatAutoSprintPreview(30, 7),
      'Example: a sprint ending 2026-08-31 is followed by 2026-09-01 – 2026-09-30, locking on 2026-09-23.',
    )
  })

  it('accepts lock days as a numeric string', () => {
    assert.equal(
      formatAutoSprintPreview('14', '3'),
      'Example: a sprint ending 2026-08-31 is followed by 2026-09-01 – 2026-09-14, locking on 2026-09-11.',
    )
  })

  it('hides the preview when lock days are invalid for the length', () => {
    assert.equal(formatAutoSprintPreview(7, 7), '')
    assert.equal(formatAutoSprintPreview(30, -1), '')
  })
})
