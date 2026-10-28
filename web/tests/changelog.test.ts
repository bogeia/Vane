import test from 'node:test'
import assert from 'node:assert/strict'
import shipped from '../src/changelog.json' with { type: 'json' }
import { createChangelogSeen, validateReleases } from '../src/lib/changelog.ts'

const release = (id: string, date: string, extra: object = {}) => ({ id, date, title: `标题 ${id}`, items: ['改动'], ...extra })

test('随前端发布的更新日志能通过校验', () => {
  assert.deepEqual(validateReleases(shipped), [])
})

test('空日志、重复 id、日期格式错、items 为空、排序错都能校验出来', () => {
  assert.deepEqual(validateReleases({ releases: [] }), ['releases 为空'])
  assert.deepEqual(validateReleases(null), ['releases 为空'])
  assert.match(validateReleases({ releases: [release('a', '2026-10-23'), release('a', '2026-10-22')] }).join(), /id a 重复/)
  for (const date of ['2026/10/23', '待定', '2026-10-2', '2026-13-45']) assert.match(validateReleases({ releases: [release('a', date)] }).join(), /不是 YYYY-MM-DD/, date)
  assert.match(validateReleases({ releases: [release('a', '2026-10-23', { items: [] })] }).join(), /至少要有一条改动/)
  assert.match(validateReleases({ releases: [release('a', '2026-10-23', { items: [''] })] }).join(), /至少要有一条改动/)
  assert.match(validateReleases({ releases: [release('a', '2026-10-23', { title: ' ' })] }).join(), /缺少标题/)
  assert.match(validateReleases({ releases: [release('a', '2026-10-22'), release('b', '2026-10-23')] }).join(), /没有按从新到旧排列/)
  assert.deepEqual(validateReleases({ releases: [release('b', '2026-10-23'), release('a', '2026-10-23'), release('c', '2026-10-22')] }), [])
})

function memoryStorage(initial?: string) {
  const values = new Map<string, string>(initial ? [['vane-changelog-seen', initial]] : [])
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
}

test('红点：没有记录时显示，记录是最新一条时不显示，记录是旧的时显示', () => {
  assert.equal(createChangelogSeen('v0.1.0', () => memoryStorage()).unseen(), true)
  assert.equal(createChangelogSeen('v0.1.0', () => memoryStorage('v0.1.0')).unseen(), false)
  assert.equal(createChangelogSeen('v0.2.0', () => memoryStorage('v0.1.0')).unseen(), true)
})

test('标记看过之后不显示，并通知所有订阅者，重新创建后仍不显示', () => {
  const storage = memoryStorage()
  const seen = createChangelogSeen('v0.1.0', () => storage)
  let calls = 0
  seen.subscribe(() => { calls++ })
  seen.markSeen()
  assert.equal(seen.unseen(), false)
  assert.equal(calls, 1)
  assert.equal(createChangelogSeen('v0.1.0', () => storage).unseen(), false)
})

test('读存储时抛错就不显示红点，写存储抛错时本次仍然隐藏', () => {
  const broken = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } }
  assert.equal(createChangelogSeen('v0.1.0', () => broken).unseen(), false)
  const readOnly = { getItem: () => null, setItem: () => { throw new Error('quota') } }
  const seen = createChangelogSeen('v0.1.0', () => readOnly)
  assert.equal(seen.unseen(), true)
  seen.markSeen()
  assert.equal(seen.unseen(), false)
})
