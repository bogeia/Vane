import test from 'node:test'
import assert from 'node:assert/strict'
import { defaultTopicView } from '../src/lib/topics.ts'

test('主题默认视图按精选与全部资料数选择', () => {
  assert.equal(defaultTopicView({ selected_count: 1, all_count: 2 }), 'selected')
  assert.equal(defaultTopicView({ selected_count: 0, all_count: 2 }), 'all')
  assert.equal(defaultTopicView({ selected_count: 0, all_count: 0 }), 'selected')
})
