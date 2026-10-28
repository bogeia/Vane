import test from 'node:test'
import assert from 'node:assert/strict'
import { tierLabel, tierOfGroup } from '../src/lib/tier.ts'

test('两个已知分级返回短名和完整说明', () => {
  assert.deepEqual(tierLabel('official'), { short: '官方', full: '官方一手' })
  assert.deepEqual(tierLabel('media'), { short: '媒体', full: '媒体与个人' })
})

test('未知代号原样返回', () => {
  assert.deepEqual(tierLabel('T3'), { short: 'T3', full: 'T3' })
  assert.deepEqual(tierLabel(''), { short: '', full: '' })
  assert.deepEqual(tierLabel('toString'), { short: 'toString', full: 'toString' })
})

test('OPML 分组标题反查短名，查不到返回 null', () => {
  assert.deepEqual(tierOfGroup('官方一手'), { short: '官方', full: '官方一手' })
  assert.deepEqual(tierOfGroup('媒体与个人'), { short: '媒体', full: '媒体与个人' })
  assert.equal(tierOfGroup('其他'), null)
})
