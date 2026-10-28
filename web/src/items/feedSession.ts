import type { createReadingState } from '../readingState'
import type { Item, View } from './types'

export type FeedQuery = { view: View; topic: string; q: string; category: string }
export type FeedPage = { items: Item[]; next_cursor?: string }
type Reading = Pick<ReturnType<typeof createReadingState>, 'subscribe' | 'isVisible' | 'unreadOnly'>
type ReadPage = (query: FeedQuery, cursor: string, signal: AbortSignal) => Promise<FeedPage>

// 请求和已读变化共同决定这次资料列表会话的可见结果及分页推进。
export function createFeedSession(readPage: ReadPage, reading: Reading) {
  let query: FeedQuery = { view: 'selected', topic: '', q: '', category: '' }
  let items: Item[] = []
  let status: 'loading' | 'failed' | 'ready' = 'loading'
  let more: 'idle' | 'loading' | 'failed' = 'idle'
  let cursor = ''
  let request: AbortController | null = null
  let started = false
  const listeners = new Set<() => void>()
  const project = () => {
    const visible = items.filter(item => reading.isVisible(item, query.view))
    const finished = status === 'ready' && cursor === ''
    return {
      items: visible, status, more, finished,
      loadingEmpty: status === 'loading' && items.length === 0,
      empty: status === 'ready' && visible.length === 0 && (finished || more === 'failed'),
      emptyBecauseRead: reading.unreadOnly() && items.length > 0,
      canLoadMore: status === 'ready' && cursor !== '',
    }
  }
  let snapshot = project()
  function publish() {
    snapshot = project()
    for (const listener of listeners) listener()
    if (reading.unreadOnly() && status === 'ready' && more === 'idle' && cursor && snapshot.items.length === 0) loadMore()
  }
  async function load(first: boolean) {
    request?.abort()
    const controller = new AbortController()
    request = controller
    const requestedQuery = query
    const requestedCursor = first ? '' : cursor
    if (first) { status = 'loading'; cursor = ''; more = 'idle' }
    else more = 'loading'
    publish()
    try {
      const page = await readPage(requestedQuery, requestedCursor, controller.signal)
      if (request !== controller || controller.signal.aborted) return
      items = first ? page.items : [...items, ...page.items]
      cursor = page.next_cursor ?? ''
      status = 'ready'
      more = 'idle'
    } catch {
      if (request !== controller || controller.signal.aborted) return
      if (first) { status = 'failed'; items = []; cursor = '' }
      else more = 'failed'
    } finally {
      if (request === controller) { request = null; publish() }
    }
  }
  function loadMore() {
    if (started && status === 'ready' && cursor && more !== 'loading' && !request) void load(false)
  }
  function setQuery(next: FeedQuery) {
    if (started && next.view === query.view && next.topic === query.topic && next.q === query.q && next.category === query.category) return
    // 切视图或主题开启新阅读范围；分类与搜索保留旧列表直到新结果返回。
    if (next.view !== query.view || next.topic !== query.topic) items = []
    query = next
    started = true
    void load(true)
  }
  return {
    snapshot: () => snapshot,
    subscribe(listener: () => void) {
      listeners.add(listener)
      const unsubscribe = reading.subscribe(publish)
      return () => { listeners.delete(listener); unsubscribe() }
    },
    setQuery,
    loadMore,
    retry: () => { void load(true) },
    dispose() { request?.abort(); request = null; started = false },
  }
}
