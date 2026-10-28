import test from 'node:test'
import assert from 'node:assert/strict'
import { createReadingState } from '../src/readingState.ts'

const item = { id: '10', title: '资料标题', source: { name: '信源' }, published_at: '2026-10-22T03:00:00Z', url: 'https://example.test/article' }
function memoryStorage() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
}

test('收藏保存完整快照、去重、按收藏时间倒序，并支持取消', () => {
  const storage = memoryStorage()
  let now = 100
  const state = createReadingState(() => storage, () => now)
  assert.equal(state.addFavorite(item), 'added')
  now = 200
  assert.equal(state.addFavorite(item), 'exists')
  assert.equal(state.addFavorite({ ...item, id: '20', title: '后收藏的资料' }), 'added')
  assert.deepEqual(state.favorites(), [
    { id: '20', title: '后收藏的资料', source_name: '信源', published_at: item.published_at, url: item.url, favorited_at: 200 },
    { id: '10', title: item.title, source_name: '信源', published_at: item.published_at, url: item.url, favorited_at: 100 },
  ])
  const restored = createReadingState(() => storage)
  assert.equal(restored.isFavorite('10'), true)
  assert.equal(restored.removeFavorite('10'), true)
  assert.equal(restored.isFavorite('10'), false)
  assert.deepEqual(createReadingState(() => storage).favorites().map(value => value.id), ['20'])
})

test('收藏满1000条拒绝新增，不淘汰；取消后可新增', () => {
  const state = createReadingState(() => memoryStorage())
  for (let i = 0; i < 1000; i++) assert.equal(state.addFavorite({ ...item, id: String(i) }), 'added')
  const before = state.favorites()
  assert.equal(state.addFavorite({ ...item, id: 'overflow' }), 'full')
  assert.deepEqual(state.favorites(), before)
  assert.equal(state.addFavorite(item), 'exists')
  state.removeFavorite('0')
  assert.equal(state.addFavorite({ ...item, id: 'overflow' }), 'added')
})

test('存储读取、写入或获取本身抛异常时，收藏退化为内存状态', () => {
  for (const storage of [
    () => { throw new Error('blocked') },
    () => ({ getItem() { throw new Error('blocked') }, setItem() { throw new Error('blocked') } }),
    () => ({ getItem: () => null, setItem() { throw new Error('quota') } }),
  ]) {
    const state = createReadingState(storage)
    assert.deepEqual(state.favorites(), [])
    assert.equal(state.addFavorite(item), 'added')
    assert.equal(state.isFavorite(item.id), true)
    assert.equal(state.removeFavorite(item.id), true)
    assert.deepEqual(state.favorites(), [])
  }
})

test('损坏或不是收藏数组的存储按空收藏处理，不让页面崩溃', () => {
  for (const value of ['{invalid', 'null', '{}', '[null]', '[{"id":"10"}]']) {
    const storage = { getItem: () => value, setItem() {} }
    const state = createReadingState(() => storage)
    assert.deepEqual(state.favorites(), [])
    assert.equal(state.addFavorite(item), 'added')
  }
})

test('已读记录保存时间并恢复，超过5000条淘汰最早记录，重读刷新时间', () => {
  const storage = memoryStorage()
  let now = 100
  const state = createReadingState(() => storage, () => now)
  state.markRead(Array.from({ length: 5000 }, (_, i) => String(i)))
  assert.equal(state.isRead('0'), true)
  now = 200
  state.markRead(['0'])
  now = 300
  state.markRead(['5000'])
  assert.equal(state.isRead('0'), true)
  assert.equal(state.isRead('1'), false)
  assert.equal(state.isRead('2'), true)
  assert.equal(state.isRead('5000'), true)
  const restored = createReadingState(() => storage)
  assert.equal(restored.isRead('0'), true)
  assert.equal(restored.isRead('1'), false)
  assert.equal(restored.isRead('5000'), true)
  assert.deepEqual(JSON.parse(storage.getItem('vane-read')!).find((entry: { id: string }) => entry.id === '0'), { id: '0', read_at: 200 })
})

test('只看未读记住开关；精选全部成员已读才隐藏，新成员让事件重新出现', () => {
  const storage = memoryStorage()
  const state = createReadingState(() => storage)
  const folded = { id: '10', story_item_ids: ['10', '11'] }
  assert.equal(state.unreadOnly(), false)
  state.markRead(['10'])
  assert.equal(state.isVisible(folded, 'all'), true)
  state.setUnreadOnly(true)
  assert.equal(createReadingState(() => storage).unreadOnly(), true)
  assert.equal(state.isVisible(folded, 'all'), false)
  assert.equal(state.isVisible({ id: '11' }, 'all'), true)
  assert.equal(state.isVisible(folded, 'selected'), true)
  state.markRead(['11'])
  assert.equal(state.isVisible(folded, 'selected'), false)
  assert.equal(state.isVisible({ ...folded, story_item_ids: ['10', '11', '12'] }, 'selected'), true)
  assert.equal(state.isVisible({ id: '10', story_item_ids: [] }, 'selected'), false)
  state.setUnreadOnly(false)
  assert.equal(state.isVisible(folded, 'selected'), true)
  assert.equal(createReadingState(() => storage).unreadOnly(), false)
})

test('已读和未读开关在存储抛错或损坏时仍可使用内存', () => {
  for (const storage of [
    () => { throw new Error('blocked') },
    () => ({ getItem() { throw new Error('blocked') }, setItem() { throw new Error('quota') } }),
    () => ({ getItem: () => 'null', setItem() { throw new Error('quota') } }),
    () => ({ getItem: () => '[null,{"id":"10"}]', setItem() { throw new Error('quota') } }),
  ]) {
    const state = createReadingState(storage)
    assert.equal(state.isRead('10'), false)
    assert.equal(state.unreadOnly(), false)
    state.markRead(['10'])
    state.setUnreadOnly(true)
    assert.equal(state.isRead('10'), true)
    assert.equal(state.isVisible({ id: '10' }, 'all'), false)
  }
})
