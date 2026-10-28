import { useLayoutEffect, useState, useSyncExternalStore } from 'react'
import { useProgress } from '../lib/resource'
import { readingState } from '../useReadingState'
import { createFeedSession, type FeedQuery, type FeedPage } from './feedSession'
import type { View } from './types'

async function readPage(query: FeedQuery, cursor: string, signal: AbortSignal): Promise<FeedPage> {
  const params = new URLSearchParams({ view: query.view, limit: '20' })
  if (query.topic) params.set('topic', query.topic)
  if (query.category) params.set('category', query.category)
  if (query.q) params.set('q', query.q)
  if (cursor) params.set('cursor', cursor)
  const response = await fetch(`/api/items?${params}`, { signal })
  if (!response.ok) throw new Error(`items ${response.status}`)
  return response.json()
}

export function useFeed(view: View, topic: string, category: string, q: string) {
  const [session] = useState(() => createFeedSession(readPage, readingState))
  const state = useSyncExternalStore(session.subscribe, session.snapshot)
  useLayoutEffect(() => { session.setQuery({ view, topic, category, q }) }, [session, view, topic, category, q])
  useLayoutEffect(() => () => session.dispose(), [session])
  useProgress(state.status === 'loading')
  return { ...state, loadMore: session.loadMore, retry: session.retry }
}
