import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { formatHours, isValidEstimate, parseEstimateInput, refinementLabel } from './estimates.ts'

describe('formatHours', () => {
  it('formats hours compactly and drops float noise', () => {
    assert.equal(formatHours(2.5), '2.5h')
    assert.equal(formatHours(0.1 + 0.2), '0.3h')
    assert.equal(formatHours(12), '12h')
    assert.equal(formatHours(0), '0h')
    assert.equal(formatHours(null), '')
    assert.equal(formatHours(undefined), '')
  })
})

describe('isValidEstimate', () => {
  it('accepts quarter hours within range only', () => {
    assert.ok(isValidEstimate(0))
    assert.ok(isValidEstimate(1.25))
    assert.ok(isValidEstimate(999.75))
    assert.ok(!isValidEstimate(1.1))
    assert.ok(!isValidEstimate(-0.25))
    assert.ok(!isValidEstimate(1000))
    assert.ok(!isValidEstimate(Number.NaN))
  })
})

describe('parseEstimateInput', () => {
  it('treats blank as cleared and rejects bad values', () => {
    assert.equal(parseEstimateInput(''), null)
    assert.equal(parseEstimateInput('  '), null)
    assert.equal(parseEstimateInput(null), null)
    assert.equal(parseEstimateInput('3.75'), 3.75)
    assert.equal(parseEstimateInput(4), 4)
    assert.equal(parseEstimateInput('1.1'), undefined)
    assert.equal(parseEstimateInput('abc'), undefined)
  })
})

describe('refinementLabel', () => {
  it('labels the two flags and leaves unset blank', () => {
    assert.equal(refinementLabel('needs_refinement'), 'Needs Refinement')
    assert.equal(refinementLabel('refined'), 'Refined')
    assert.equal(refinementLabel(''), '')
    assert.equal(refinementLabel(undefined), '')
  })
})
