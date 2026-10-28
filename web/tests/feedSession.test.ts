import test from 'node:test'
import assert from 'node:assert/strict'
import { createFeedSession, type FeedPage, type FeedQuery } from '../src/items/feedSession.ts'
import { createReadingState } from '../src/readingState.ts'
import type { Item } from '../src/items/types.ts'

const query: FeedQuery = { view: 'selected', topic: '', q: '', category: '' }
const item = (id: string): Item => ({ id, story_item_ids: [id] } as Item)
const tick = () => new Promise(resolve => setImmediate(resolve))
function fixture() {
  const requests: { query: FeedQuery; cursor: string; signal: AbortSignal; resolve: (page: FeedPage) => void; reject: (error: Error) => void }[] = []
  const reading = createReadingState(() => ({ getItem: () => null, setItem() {} }))
  const session = createFeedSession((query, cursor, signal) => new Promise<FeedPage>((resolve, reject) => {
    requests.push({ query, cursor, signal, resolve, reject })
  }), reading)
  const unsubscribe = session.subscribe(() => {})
  return { requests, reading, session, stop() { unsubscribe(); session.dispose() } }
}

test('分类与搜索保留旧列表，范围切换清空；取消及晚到响应不覆盖当前查询', async () => {
  const f = fixture()
  try {
    f.session.setQuery(query)
    f.requests[0].resolve({ items: [item('old')], next_cursor: 'next' })
    await tick()
    f.session.setQuery({ ...query, category: '行业' })
    assert.equal(f.session.snapshot().items[0].id, 'old')
    assert.equal(f.session.snapshot().loadingEmpty, false)
    f.session.setQuery({ ...query, q: 'Claude' })
    assert.equal(f.requests[1].signal.aborted, true)
    f.requests[2].resolve({ items: [item('search')] })
    await tick()
    f.requests[1].resolve({ items: [item('late')] })
    await tick()
    assert.deepEqual(f.session.snapshot().items.map(item => item.id), ['search'])
    for (const next of [{ ...query, view: 'all' as const }, { ...query, topic: 'openai' }]) {
      f.session.setQuery(next)
      assert.equal(f.session.snapshot().loadingEmpty, true)
      assert.deepEqual(f.session.snapshot().items, [])
      f.requests.at(-1)!.resolve({ items: [item('scope')] })
      await tick()
    }
  } finally { f.stop() }
})

test('分页失败保留资料，同游标重试；重复点击只请求一次', async () => {
  const f = fixture()
  try {
    f.session.setQuery(query)
    f.requests[0].resolve({ items: [item('first')], next_cursor: 'page-2' })
    await tick()
    f.session.loadMore(); f.session.loadMore()
    assert.equal(f.requests.length, 2)
    f.requests[1].reject(new Error('offline'))
    await tick()
    assert.equal(f.session.snapshot().more, 'failed')
    assert.equal(f.session.snapshot().items[0].id, 'first')
    f.session.loadMore()
    assert.equal(f.requests[2].cursor, 'page-2')
    f.requests[2].resolve({ items: [item('second')] })
    await tick()
    assert.deepEqual(f.session.snapshot().items.map(item => item.id), ['first', 'second'])
    assert.equal(f.session.snapshot().finished, true)
    assert.equal(f.session.snapshot().canLoadMore, false)
  } finally { f.stop() }
})

test('已读开关和阅读变化自动补页，遇到未读或末页停止', async () => {
  const f = fixture()
  try {
    f.reading.markRead(['first', 'second'])
    f.session.setQuery(query)
    f.requests[0].resolve({ items: [item('first')], next_cursor: 'page-2' })
    await tick()
    assert.equal(f.requests.length, 1)
    f.reading.setUnreadOnly(true)
    assert.equal(f.requests[1].cursor, 'page-2')
    f.requests[1].resolve({ items: [item('second')], next_cursor: 'page-3' })
    await tick()
    assert.equal(f.requests[2].cursor, 'page-3')
    f.requests[2].resolve({ items: [item('third')], next_cursor: 'page-4' })
    await tick()
    assert.equal(f.requests.length, 3)
    assert.deepEqual(f.session.snapshot().items.map(item => item.id), ['third'])
    f.reading.markRead(['third'])
    assert.equal(f.requests[3].cursor, 'page-4')
    f.requests[3].resolve({ items: [] })
    await tick()
    assert.equal(f.session.snapshot().empty, true)
    assert.equal(f.session.snapshot().emptyBecauseRead, true)
    assert.equal(f.session.snapshot().finished, true)
    f.reading.setUnreadOnly(false)
    assert.equal(f.session.snapshot().items.length, 3)
    assert.equal(f.requests.length, 4)
  } finally { f.stop() }
})

test('自动补页失败不循环请求，第一页失败可重试', async () => {
  const f = fixture()
  try {
    f.session.setQuery(query)
    f.requests[0].reject(new Error('offline'))
    await tick()
    assert.equal(f.session.snapshot().status, 'failed')
    f.session.retry()
    f.reading.markRead(['first']); f.reading.setUnreadOnly(true)
    f.requests[1].resolve({ items: [item('first')], next_cursor: 'next' })
    await tick()
    f.requests[2].reject(new Error('offline'))
    await tick()
    assert.equal(f.session.snapshot().empty, true)
    assert.equal(f.session.snapshot().more, 'failed')
    f.reading.markRead(['other'])
    assert.equal(f.requests.length, 3)
  } finally { f.stop() }
})

test('取消旧分页并忽略晚到结果；卸载后同查询可重新启动', async () => {
  const f = fixture()
  try {
    f.session.setQuery(query)
    f.requests[0].resolve({ items: [item('old')], next_cursor: 'next' })
    await tick()
    f.session.loadMore()
    f.session.setQuery({ ...query, q: 'new' })
    assert.equal(f.requests[1].signal.aborted, true)
    f.requests[1].resolve({ items: [item('late-page')] })
    f.requests[2].resolve({ items: [item('new')] })
    await tick()
    assert.deepEqual(f.session.snapshot().items.map(item => item.id), ['new'])
    f.session.setQuery({ ...query, q: 'pending' })
    f.session.dispose()
    assert.equal(f.requests[3].signal.aborted, true)
    f.requests[3].resolve({ items: [item('unmounted')] })
    await tick()
    assert.equal(f.session.snapshot().items[0].id, 'new')
    f.session.setQuery({ ...query, q: 'pending' })
    assert.equal(f.requests.length, 5)
    f.requests[4].resolve({ items: [item('final')], next_cursor: 'next' })
    await tick()
    f.session.dispose()
    f.reading.markRead(['final']); f.reading.setUnreadOnly(true)
    assert.equal(f.requests.length, 5, '卸载后已读变化不能再触发补页')
  } finally { f.stop() }
})
