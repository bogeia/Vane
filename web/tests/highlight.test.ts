import test from 'node:test'
import assert from 'node:assert/strict'
import { highlight, type Segment } from '../src/lib/highlight.ts'

const hits = (segments: Segment[]) => segments.filter(segment => segment.hit).map(segment => segment.text)
const joined = (segments: Segment[]) => segments.map(segment => segment.text).join('')

test('中文命中，命中在开头和结尾都能标出', () => {
  const text = '智能体发布新的智能体'
  const segments = highlight(text, '智能体')
  assert.deepEqual(segments, [{ text: '智能体', hit: true }, { text: '发布新的', hit: false }, { text: '智能体', hit: true }])
  assert.equal(joined(segments), text)
})

test('英文不区分大小写，保留原文大小写，同一段多处命中', () => {
  const text = 'OpenAI and openai and OPENAI'
  const segments = highlight(text, 'OpenAi')
  assert.deepEqual(hits(segments), ['OpenAI', 'openai', 'OPENAI'])
  assert.equal(joined(segments), text)
})

test('%、_、反斜杠和正则特殊字符按字面匹配', () => {
  for (const [text, query] of [['增长 50% 以上', '50%'], ['a_b 和 ab', 'a_b'], ['路径 C:\\temp', 'C:\\temp'], ['价格 (1.5+x)*2', '(1.5+x)*'], ['[规则] ^$', '[规则] ^$']]) {
    const segments = highlight(text, query)
    assert.deepEqual(hits(segments), [query], `${text} / ${query}`)
    assert.equal(joined(segments), text)
  }
  assert.deepEqual(hits(highlight('abc', '.')), [])
  assert.deepEqual(hits(highlight('a1b', 'a_b')), [])
})

test('搜索词为空或搜不到时原样返回一段', () => {
  assert.deepEqual(highlight('原文', ''), [{ text: '原文', hit: false }])
  assert.deepEqual(highlight('原文', '没有'), [{ text: '原文', hit: false }])
  assert.deepEqual(highlight('', '词'), [{ text: '', hit: false }])
})

test('小写后长度会变的字符不高亮，也不抛错', () => {
  assert.deepEqual(highlight('İstanbul AI', 'ai'), [{ text: 'İstanbul AI', hit: false }])
  assert.deepEqual(highlight('Istanbul', 'İ'), [{ text: 'Istanbul', hit: false }])
})

test('相邻和重叠的命中按从左到右不重叠地标出', () => {
  const segments = highlight('aaaa', 'aa')
  assert.deepEqual(segments, [{ text: 'aa', hit: true }, { text: 'aa', hit: true }])
  assert.equal(joined(highlight('xaaay', 'aa')), 'xaaay')
})
