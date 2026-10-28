import { useSyncExternalStore } from 'react'
import { useProgress } from './resource'
import { createSharedRead } from './sharedRead'

type Topic = { slug: string; name: string; group: string; description: string; selected_count: number; all_count: number }
type ReportSummary = { date: string; headline: string }
const reports = createSharedRead<{ items: ReportSummary[] }>('/api/reports')
const topics = createSharedRead<{ topics: Topic[] }>('/api/topics')

export function useReports({ progress = true } = {}) {
  const result = useSyncExternalStore(reports.subscribe, reports.getSnapshot)
  useProgress(progress && result.status === 'loading')
  return { ...result, retry: reports.refresh }
}

export function useTopics({ progress = true } = {}) {
  const result = useSyncExternalStore(topics.subscribe, topics.getSnapshot)
  useProgress(progress && result.status === 'loading')
  return { ...result, retry: topics.refresh }
}

// Shell 跨页常驻：每次路由进入重验活跃读取，不让导航的订阅永久锁住旧日报。
export function refreshReading() {
  reports.refresh()
  topics.refresh()
}
