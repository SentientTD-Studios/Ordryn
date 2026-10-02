import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { applyFormat, handleListEnter, renderMarkdown } from './markdown.ts'

describe('renderMarkdown', () => {
  it('renders bold, italic, and underline', () => {
    const html = renderMarkdown('**bold text** and *italic text* and <u>underlined text</u>')
    assert.match(html, /<strong>bold text<\/strong>/)
    assert.match(html, /<em>italic text<\/em>/)
    assert.match(html, /<u>underlined text<\/u>/)
  })

  it('renders unordered and ordered lists', () => {
    const ulHtml = renderMarkdown('- Item 1\n- Item 2')
    assert.match(ulHtml, /<ul>/)
    assert.match(ulHtml, /<li>Item 1<\/li>/)
    assert.match(ulHtml, /<li>Item 2<\/li>/)

    const olHtml = renderMarkdown('1. First\n2. Second')
    assert.match(olHtml, /<ol>/)
    assert.match(olHtml, /<li>First<\/li>/)
    assert.match(olHtml, /<li>Second<\/li>/)
  })

  it('renders safe links with target blank and rel', () => {
    const html = renderMarkdown('[Google](https://google.com)')
    assert.match(html, /<a href="https:\/\/google\.com" target="_blank" rel="noopener noreferrer" class="rich-body-link">Google<\/a>/)
  })

  it('rejects unsafe javascript links', () => {
    const html = renderMarkdown('[Click me](javascript:alert(1))')
    assert.doesNotMatch(html, /<a href="javascript/)
    assert.match(html, /Click me/)
  })

  it('renders safe images', () => {
    const html = renderMarkdown('![my picture](https://example.com/pic.png)')
    assert.match(html, /<img class="rich-body-image" src="https:\/\/example\.com\/pic\.png" alt="my picture"/)
  })

  it('does not render unsafe image sources', () => {
    for (const src of ['javascript:alert(1)', 'data:image/png;base64,aaa', '//evil.example/x.png']) {
      const html = renderMarkdown(`![x](${src})`)
      assert.doesNotMatch(html, /<img/, src)
      assert.doesNotMatch(html, /javascript:|data:image|evil\.example/, src)
      assert.match(html, /\[image: x\]/, src)
    }
  })

  it('renders mentions with class', () => {
    const html = renderMarkdown('Hey @alice how are you?')
    assert.match(html, /<span class="rich-body-mention">@alice<\/span>/)
  })

  it('renders task references with interactive buttons', () => {
    const html = renderMarkdown('See #42 and [[100]]', {
      taskTitle: (id) => (id === 42 ? 'Fix bug' : undefined),
    })
    assert.match(html, /<button type="button" class="rich-body-task-link" data-task-id="42" title="Open task #42">Task #42 - Fix bug<\/button>/)
    assert.match(html, /<button type="button" class="rich-body-task-link" data-task-id="100" title="Open task #100">Task #100<\/button>/)
  })

  it('escapes arbitrary script tags', () => {
    const html = renderMarkdown('<script>alert("xss")</script>')
    assert.doesNotMatch(html, /<script>/)
    assert.match(html, /&lt;script&gt;/)
  })

  it('rejects data and vbscript URLs in links', () => {
    const dataHtml = renderMarkdown('[click](data:text/html,<script>alert(1)</script>)')
    assert.doesNotMatch(dataHtml, /<a href="data:/)
    assert.match(dataHtml, /click/)

    const vbHtml = renderMarkdown('[click](vbscript:msgbox(1))')
    assert.doesNotMatch(vbHtml, /<a href="vbscript:/)
    assert.match(vbHtml, /click/)
  })

  it('rejects path traversal in /uploads/ links', () => {
    const html = renderMarkdown('[secret](/uploads/../../etc/passwd)')
    assert.doesNotMatch(html, /<a href="\/uploads\/\.\.\/\.\.\/etc\/passwd"/)
    assert.match(html, /secret/)
  })

  it('escapes dangerous iframe and svg onload tags', () => {
    const iframeHtml = renderMarkdown('<iframe src="https://evil.com"></iframe>')
    assert.doesNotMatch(iframeHtml, /<iframe/)

    const svgHtml = renderMarkdown('<svg onload="alert(1)">')
    assert.doesNotMatch(svgHtml, /<svg/)
  })

  it('renders headings, code blocks, and blockquotes', () => {
    const headingHtml = renderMarkdown('# Main Title\n## Subtitle')
    assert.match(headingHtml, /<h1>Main Title<\/h1>/)
    assert.match(headingHtml, /<h2>Subtitle<\/h2>/)

    const codeHtml = renderMarkdown('Use `const x = 42` in your code')
    assert.match(codeHtml, /<code>const x = 42<\/code>/)

    const fenceHtml = renderMarkdown('```js\nconsole.log("hi")\n```')
    assert.match(fenceHtml, /<pre class="hljs"><code class="hljs language-js">/)
    assert.match(fenceHtml, /<span class="hljs-string">/)

    const plainFence = renderMarkdown('```\n<b>a</b> && b\n```')
    assert.match(plainFence, /<pre><code>&lt;b&gt;a&lt;\/b&gt; &amp;&amp; b<\/code><\/pre>/)

    const unknownLang = renderMarkdown('```klingon\n<b>hi</b>\n```')
    assert.match(unknownLang, /<pre><code class="language-klingon">&lt;b&gt;hi&lt;\/b&gt;<\/code><\/pre>/)

    const htmlFence = renderMarkdown('```html\n<div class="x">&amp;</div>\n```')
    assert.doesNotMatch(htmlFence, /&amp;lt;/)
    assert.doesNotMatch(htmlFence, /<div/)
  })

  // No DOMPurify under node, so these exercise the renderer's own escaping.
  it('escapes code content without relying on DOMPurify', () => {
    const payloads = [
      '```html\n<script>alert(1)</script>\n```',
      '```js\n</code></pre><img src=x onerror=alert(1)>\n```',
      '```\n</code></pre><img src=x onerror=alert(1)>\n```',
      '```klingon\n</code></pre><img src=x onerror=alert(1)>\n```',
      '```"><img src=x onerror=alert(1)>\nx\n```',
      '```js"onmouseover="alert(1)\nx\n```',
      '```html\n&lt;script&gt;alert(1)&lt;/script&gt;\n```',
      '```\n&lt;img src=x onerror=alert(1)&gt;\n```',
      '~~~xml\n<svg onload=alert(1)>\n~~~',
      '    </code></pre><script>alert(1)</script>',
      'a `</code><script>alert(1)</script>` b',
    ]
    for (const md of payloads) {
      const html = renderMarkdown(md)
      assert.doesNotMatch(html, /<(script|img|svg)\b/i, md)
      assert.doesNotMatch(html, /<[^>]*\son\w+=/i, md)
    }

    const quoteHtml = renderMarkdown('> Important warning')
    assert.match(quoteHtml, /<blockquote>/)
    assert.match(quoteHtml, /Important warning/)
  })

  it('handles empty and whitespace-only body', () => {
    assert.equal(renderMarkdown(''), '')
    assert.equal(renderMarkdown('   \n\t  '), '')
  })

  it('handles mentions with trailing punctuation', () => {
    const html = renderMarkdown('Hey @alice, check with @bob!')
    assert.match(html, /<span class="rich-body-mention">@alice<\/span>, check with <span class="rich-body-mention">@bob<\/span>!/)
  })
})

describe('applyFormat', () => {
  it('wraps and unwraps bold', () => {
    const wrap = applyFormat('hello world', 0, 5, 'bold')
    assert.equal(wrap.text, '**hello** world')

    const unwrap = applyFormat('**hello** world', 2, 7, 'bold')
    assert.equal(unwrap.text, 'hello world')
  })

  it('inserts default bold when nothing selected', () => {
    const res = applyFormat('hello ', 6, 6, 'bold')
    assert.equal(res.text, 'hello **bold text**')
    assert.equal(res.selectionStart, 8)
    assert.equal(res.selectionEnd, 17)
  })

  it('wraps and unwraps italic', () => {
    const wrap = applyFormat('hello world', 0, 5, 'italic')
    assert.equal(wrap.text, '*hello* world')

    const unwrap = applyFormat('*hello* world', 1, 6, 'italic')
    assert.equal(unwrap.text, 'hello world')
  })

  it('wraps and unwraps underline', () => {
    const wrap = applyFormat('hello world', 0, 5, 'underline')
    assert.equal(wrap.text, '<u>hello</u> world')

    const unwrap = applyFormat('<u>hello</u> world', 3, 8, 'underline')
    assert.equal(unwrap.text, 'hello world')
  })

  it('toggles bullet lists', () => {
    const res = applyFormat('Apple\nBanana', 0, 12, 'ul')
    assert.equal(res.text, '- Apple\n- Banana')

    const untoggle = applyFormat('- Apple\n- Banana', 0, 16, 'ul')
    assert.equal(untoggle.text, 'Apple\nBanana')
  })

  it('inserts bullet item without trailing newline when empty', () => {
    const res = applyFormat('', 0, 0, 'ul')
    assert.equal(res.text, '- List item')
    assert.equal(res.selectionStart, 2)
    assert.equal(res.selectionEnd, 11)
  })

  it('toggles ordered lists', () => {
    const res = applyFormat('Apple\nBanana', 0, 12, 'ol')
    assert.equal(res.text, '1. Apple\n2. Banana')

    const untoggle = applyFormat('1. Apple\n2. Banana', 0, 18, 'ol')
    assert.equal(untoggle.text, 'Apple\nBanana')
  })

  it('inserts numbered item without trailing newline when empty', () => {
    const res = applyFormat('', 0, 0, 'ol')
    assert.equal(res.text, '1. List item')
    assert.equal(res.selectionStart, 3)
    assert.equal(res.selectionEnd, 12)
  })

  it('formats link', () => {
    const res = applyFormat('hello website world', 6, 13, 'link', { url: 'https://example.com' })
    assert.equal(res.text, 'hello [website](https://example.com) world')
  })
})

describe('handleListEnter', () => {
  it('continues unordered list items with hyphen, asterisk, or plus', () => {
    const hyphen = handleListEnter('- First', 7, 7)
    assert.deepEqual(hyphen, { text: '- First\n- ', cursor: 10 })

    const asterisk = handleListEnter('* First', 7, 7)
    assert.deepEqual(asterisk, { text: '* First\n* ', cursor: 10 })

    const plus = handleListEnter('+ First', 7, 7)
    assert.deepEqual(plus, { text: '+ First\n+ ', cursor: 10 })
  })

  it('preserves indentation for nested unordered lists', () => {
    const res = handleListEnter('  - Sub item', 12, 12)
    assert.deepEqual(res, { text: '  - Sub item\n  - ', cursor: 17 })
  })

  it('continues task list checkboxes', () => {
    const unchecked = handleListEnter('- [ ] Write tests', 17, 17)
    assert.deepEqual(unchecked, { text: '- [ ] Write tests\n- [ ] ', cursor: 24 })

    const checked = handleListEnter('- [x] Done item', 15, 15)
    assert.deepEqual(checked, { text: '- [x] Done item\n- [ ] ', cursor: 22 })
  })

  it('continues ordered list with incremented number', () => {
    const single = handleListEnter('1. Buy milk', 11, 11)
    assert.deepEqual(single, { text: '1. Buy milk\n2. ', cursor: 15 })

    const doubleDigit = handleListEnter('9. Item nine', 12, 12)
    assert.deepEqual(doubleDigit, { text: '9. Item nine\n10. ', cursor: 17 })

    const indented = handleListEnter('  1. Nested one', 15, 15)
    assert.deepEqual(indented, { text: '  1. Nested one\n  2. ', cursor: 21 })
  })

  it('renumbers subsequent ordered list items', () => {
    const initial = '1. First\n2. Second\n3. Third'
    // Cursor at end of line 1
    const res = handleListEnter(initial, 8, 8)
    assert.ok(res)
    assert.equal(res.text, '1. First\n2. \n3. Second\n4. Third')
    assert.equal(res.cursor, 12)
  })

  it('splits list item if enter pressed in middle of text', () => {
    const ulSplit = handleListEnter('- Apples and oranges', 8, 8)
    assert.deepEqual(ulSplit, { text: '- Apples\n-  and oranges', cursor: 11 })

    const olSplit = handleListEnter('1. Apples and oranges', 9, 9)
    assert.deepEqual(olSplit, { text: '1. Apples\n2.  and oranges', cursor: 13 })
  })

  it('exits unordered list when pressing enter on an empty item', () => {
    const text = '- Item 1\n- '
    const res = handleListEnter(text, 11, 11)
    assert.deepEqual(res, { text: '- Item 1\n', cursor: 9 })
  })

  it('exits ordered list when pressing enter on an empty item', () => {
    const text = '1. Item 1\n2. '
    const res = handleListEnter(text, 13, 13)
    assert.deepEqual(res, { text: '1. Item 1\n', cursor: 10 })
  })

  it('unindents empty nested list items by 2 spaces', () => {
    const ulNested = '  - '
    const ulRes = handleListEnter(ulNested, 4, 4)
    assert.deepEqual(ulRes, { text: '- ', cursor: 2 })

    const olNested = '  1. '
    const olRes = handleListEnter(olNested, 5, 5)
    assert.deepEqual(olRes, { text: '1. ', cursor: 3 })
  })

  it('exits completely when single empty list item in document', () => {
    const resUl = handleListEnter('- ', 2, 2)
    assert.deepEqual(resUl, { text: '', cursor: 0 })

    const resOl = handleListEnter('1. ', 3, 3)
    assert.deepEqual(resOl, { text: '', cursor: 0 })

    const resTask = handleListEnter('- [ ] ', 6, 6)
    assert.deepEqual(resTask, { text: '', cursor: 0 })
  })

  it('returns null for non-list content', () => {
    assert.equal(handleListEnter('Hello world', 11, 11), null)
    assert.equal(handleListEnter('---', 3, 3), null)
    assert.equal(handleListEnter('123 Numbers without dot', 23, 23), null)
    assert.equal(handleListEnter('1. First item', 0, 0), null)
  })
})
