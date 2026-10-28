import test from 'node:test'
import assert from 'node:assert/strict'
import { adjacentIssues } from '../src/lib/issues.ts'

const dates = ['2026-10-23', '2026-10-22', '2026-10-21']

test('在中间时两边都有', () => {
  assert.deepEqual(adjacentIssues(dates, '2026-10-22'), { older: '2026-10-21', newer: '2026-10-23' })
})

test('最新一期没有更新的，最早一期没有更早的', () => {
  assert.deepEqual(adjacentIssues(dates, '2026-10-23'), { older: '2026-10-22', newer: undefined })
  assert.deepEqual(adjacentIssues(dates, '2026-10-21'), { older: undefined, newer: '2026-10-22' })
})

test('当前日期不在列表里或列表为空时两边都没有', () => {
  assert.deepEqual(adjacentIssues(dates, '2026-10-20'), {})
  assert.deepEqual(adjacentIssues([], '2026-10-23'), {})
})
