import test from 'node:test'
import assert from 'node:assert/strict'
import { visibleDiscussion } from '../src/pages/discussion.ts'

const discussion = {
  thread_url: 'https://news.ycombinator.com/item?id=123', sample_size: 10,
  updated_at: '2026-11-03T08:00:00Z',
  points: [{ text: '有评论者关注适用限制。', comment_url: 'https://news.ycombinator.com/item?id=456' }],
}

test('无讨论、空要点或无效元信息时不返回可展示观点', () => {
  for (const value of [null, undefined, { ...discussion, points: [] }, { ...discussion, sample_size: 1 }, { ...discussion, updated_at: 'invalid' }, { ...discussion, points: [{ ...discussion.points[0], text: ' ' }] }]) assert.equal(visibleDiscussion(value), null)
})

test('拒绝不可信出处，仅显示有出处的最多三条采样观点', () => {
  for (const url of ['javascript:alert(1)', 'https://news.ycombinator.com.evil/item?id=1', 'https://evil.com/item?id=1', 'https://user@news.ycombinator.com/item?id=1', 'https://news.ycombinator.com/item?id=0']) {
    assert.equal(visibleDiscussion({ ...discussion, thread_url: url }), null)
    assert.equal(visibleDiscussion({ ...discussion, points: [{ text: '观点', comment_url: url }] }), null)
  }
  const result = visibleDiscussion({ ...discussion, points: [...Array(4).fill(discussion.points[0]), { text: '无出处', comment_url: 'https://evil.com' }] })
  assert.equal(result?.points.length, 3)
  assert.equal(result?.sample_size, 10)
  assert.deepEqual(visibleDiscussion(discussion), discussion)
})
