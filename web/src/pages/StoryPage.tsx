import { useEffect } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { SourceAvatar } from '../items/ItemCard'
import type { Item } from '../items/types'
import { useResource } from '../lib/resource'
import { fullTime } from '../lib/time'
import { useReadingState } from '../useReadingState'
import { ErrorPage, SkeletonRows } from '../ui/States'
import { DiscussionSection } from './DiscussionSection'
import type { StoryDiscussion } from './discussion'
import './detail.css'

export type Story = { id: string; title: string; items: Item[]; discussion: StoryDiscussion | null }

// 事件页 /stories/:id：同一事件的所有公开资料，按北京时间从早到晚排成时间线。打开即记为已读。
export function StoryPage() {
  const { id = '' } = useParams()
  const { hash } = useLocation()
  const reading = useReadingState()
  const resource = useResource<Story>(`/api/stories/${encodeURIComponent(id)}`)
  const story = resource.status === 'ok' ? resource.data : null
  useEffect(() => { if (story) reading.markRead(story.items.map(item => item.id)) }, [story, reading])

  useEffect(() => {
    if (story && hash === '#discussion') document.getElementById('discussion')?.scrollIntoView({ behavior: 'instant' })
  }, [story, hash])

  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind={resource.status} what="事件" onRetry={resource.retry} />
  if (!story) return <div className="detail"><SkeletonRows /></div>
  return <article className="detail">
    <nav className="crumb" aria-label="位置"><Link to="/hot">热点</Link><span>/</span><span aria-current="page">事件时间线</span></nav>
    <h1>{story.title}</h1>
    <p className="detail-original">事件时间线 · {story.items.length} 条资料，按北京时间从早到晚</p>
    <DiscussionSection discussion={story.discussion} />
    <ol className="timeline">
      {story.items.map(item => <li key={item.id} data-shortcut-item>
        <p className="timeline-meta"><time dateTime={item.published_at}>{fullTime(item.published_at)}</time><span><SourceAvatar name={item.source.name} iconURL={item.source.icon_url} />{item.source.name}</span></p>
        <h2><Link to={`/items/${item.id}`} data-shortcut-title>{item.title}</Link></h2>
        <p className="timeline-summary">{item.summary}</p>
        <a className="timeline-link" href={item.url} target="_blank" rel="noopener noreferrer" onClick={() => reading.markRead([item.id])} data-shortcut-original>原文 ↗<span className="visually-hidden">（新标签页）</span></a>
      </li>)}
    </ol>
  </article>
}
