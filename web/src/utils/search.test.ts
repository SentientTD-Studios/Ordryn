import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  editDistance,
  freeTextTerms,
  highlightSegments,
  isFuzzyTerm,
  matchedInComments,
  searchExcerpt,
  searchTaskId,
  taskMatchesKeyword,
  tokenizeSearch,
} from './search.ts'

describe('tokenizeSearch', () => {
  it('splits on spaces outside quotes', () => {
    assert.deepEqual(tokenizeSearch('  fix "the login" project:"web app" '), ['fix', 'the login', 'project:web app'])
  })
})

describe('freeTextTerms', () => {
  it('drops qualifiers and #ids but keeps unknown prefixes', () => {
    assert.deepEqual(freeTextTerms('login #12 tag:bug is:open http://x.io'), ['login', 'http://x.io'])
  })
})

describe('searchTaskId', () => {
  it('reads #123 and 123', () => {
    assert.equal(searchTaskId('#123'), 123)
    assert.equal(searchTaskId(' 42 '), 42)
    assert.equal(searchTaskId('12a'), null)
    assert.equal(searchTaskId('#0'), null)
  })
})

describe('taskMatchesKeyword', () => {
  const task = { id: 7, title: 'Fix login page', description: 'Users see a 500', tags: [{ id: 1, name: 'Backend' }] as never }
  it('needs every word, anywhere', () => {
    assert.equal(taskMatchesKeyword(task, 'login backend'), true)
    assert.equal(taskMatchesKeyword(task, 'LOGIN 500'), true)
    assert.equal(taskMatchesKeyword(task, 'login frontend'), false)
  })
  it('matches the id', () => {
    assert.equal(taskMatchesKeyword(task, '#7'), true)
    assert.equal(taskMatchesKeyword(task, '8'), false)
  })
  it('matches everything when blank', () => {
    assert.equal(taskMatchesKeyword(task, '  '), true)
  })
})

describe('highlightSegments', () => {
  it('marks every occurrence, merging overlaps', () => {
    assert.deepEqual(highlightSegments('Login and log', ['log', 'login']), [
      { text: 'Login', match: true },
      { text: ' and ', match: false },
      { text: 'log', match: true },
    ])
  })
  it('returns the text whole without terms', () => {
    assert.deepEqual(highlightSegments('abc', []), [{ text: 'abc', match: false }])
  })
})

it('editDistance counts swaps as one edit', () => {
  assert.equal(editDistance('recieve', 'receive'), 1)
  assert.equal(editDistance('lgoin', 'login'), 1)
  assert.equal(editDistance('kitten', 'sitting'), 3)
  assert.equal(editDistance('', 'web'), 3)
})

it('isFuzzyTerm needs a five-letter word', () => {
  assert.equal(isFuzzyTerm('login'), true)
  assert.equal(isFuzzyTerm('test'), false)
  assert.equal(isFuzzyTerm('12345'), false)
  assert.equal(isFuzzyTerm('e-mail'), false)
})

it('highlightSegments marks words one typo from the term', () => {
  const marked = (text: string, terms: string[]) =>
    highlightSegments(text, terms).filter((s) => s.match).map((s) => s.text)
  assert.deepEqual(marked('Receive vendor invoices', ['recieve']), ['Receive'])
  assert.deepEqual(marked('Fix the login page', ['lgoin']), ['login'])
  assert.deepEqual(marked('Best rest', ['tset']), [])
  assert.deepEqual(marked('Receivables', ['recieve']), [])
})

describe('searchExcerpt', () => {
  it('centers on the first match and strips markup', () => {
    const desc = '**Intro** ' + 'x '.repeat(100) + 'the needle is here ' + 'y '.repeat(100)
    const out = searchExcerpt(desc, ['needle'], 40)
    assert.ok(out.includes('needle'))
    assert.equal(out.startsWith('…'), true)
    assert.ok(!out.includes('*'))
  })
  it('keeps short descriptions whole', () => {
    assert.equal(searchExcerpt('short', ['x']), 'short')
  })
})

describe('matchedInComments', () => {
  const task = { id: 7, title: 'Fix login page', description: 'Users see a 500', tags: [{ id: 1, name: 'Backend' }] as never }
  it('is false when the visible fields explain the match', () => {
    assert.equal(matchedInComments(task, ['login', 'backend', '500']), false)
    assert.equal(matchedInComments(task, ['lgoin']), false)
    assert.equal(matchedInComments(task, ['#7']), false)
  })
  it('is true when a word only matched elsewhere', () => {
    assert.equal(matchedInComments(task, ['login', 'certificate']), true)
  })
})
