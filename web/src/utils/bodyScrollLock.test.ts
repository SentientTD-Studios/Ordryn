import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { acquireScrollLock, scrollLockHolders } from './bodyScrollLock.ts'

function fakeBody() {
  const classes = new Set<string>()
  return {
    classes,
    classList: { add: (t: string) => classes.add(t), remove: (t: string) => classes.delete(t) },
    style: { overflow: '' },
  }
}

describe('acquireScrollLock', () => {
  it('locks while held and unlocks on release', () => {
    const body = fakeBody()
    const release = acquireScrollLock(body)
    assert.equal(body.style.overflow, 'hidden')
    assert.ok(body.classes.has('modal-open'))
    release()
    assert.equal(body.style.overflow, '')
    assert.ok(!body.classes.has('modal-open'))
    assert.equal(scrollLockHolders(), 0)
  })

  it('stays locked until the last nested modal releases', () => {
    const body = fakeBody()
    const releaseTask = acquireScrollLock(body)
    const releaseConfirm = acquireScrollLock(body)
    releaseConfirm()
    assert.equal(body.style.overflow, 'hidden')
    releaseTask()
    assert.equal(body.style.overflow, '')
  })

  it('unlocks regardless of close order (confirm outlives the task modal)', () => {
    const body = fakeBody()
    const releaseTask = acquireScrollLock(body)
    const releaseConfirm = acquireScrollLock(body)
    releaseTask()
    assert.equal(body.style.overflow, 'hidden')
    releaseConfirm()
    assert.equal(body.style.overflow, '')
  })

  it('ignores double release', () => {
    const body = fakeBody()
    const releaseA = acquireScrollLock(body)
    const releaseB = acquireScrollLock(body)
    releaseA()
    releaseA()
    assert.equal(body.style.overflow, 'hidden')
    releaseB()
    assert.equal(scrollLockHolders(), 0)
  })
})
