import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { Feed } from '../items/Feed'
import type { View } from '../items/types'
import { defaultTopicView } from '../lib/topics'
import { useTopics } from '../lib/reading'
import { Columns } from '../shell/Shell'
import { HomeRail } from '../ui/HomeRail'
import { IconRss } from '../ui/icons'
import { ErrorPage, SkeletonCards } from '../ui/States'
import '../ui/page.css'
import '../ui/view-switch.css'
import './topics.css'

const GROUPS = [['company', '公司'], ['field', '方向'], ['genre', '内容形态']] as const

// 主题索引 /topics：按公司、方向、内容形态分三组。
export function TopicsPage() {
  const resource = useTopics()
  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind="failed" what="主题" onRetry={resource.retry} />
  return <>
    <header className="page-head"><div><h1>主题</h1><p>按公司、方向和内容形态追踪 AI 动态，查看各主题的精选与全部资料数。</p></div></header>
    {resource.status === 'loading' && <SkeletonCards />}
    {resource.status === 'ok' && GROUPS.map(([group, name]) => {
      const topics = resource.data.topics.filter(topic => topic.group === group)
      return topics.length > 0 && <section className="topic-group" key={group} aria-labelledby={`topics-${group}`}>
        <h2 id={`topics-${group}`}>{name}</h2>
        <ul className="topic-grid">{topics.map(topic => <li key={topic.slug}>
          <Link className="topic-card" to={`/topics/${topic.slug}`}>
            <span className="topic-name">{group === 'company' && <img className="topic-logo" src={`/logos/companies/${topic.slug}.svg`} alt="" width="24" height="24" />}{topic.name}</span>
            <span className="topic-description">{topic.description}</span>
            <span className="topic-count">精选 {topic.selected_count} · 全部 {topic.all_count}</span>
          </Link>
        </li>)}</ul>
      </section>
    })}
  </>
}

// 主题页 /topics/:slug。换主题时重置精选/全部切换。
export function TopicPage() {
  const { slug = '' } = useParams()
  return <Columns rail={<HomeRail excludeTopic={slug} />}><TopicView slug={slug} /></Columns>
}

function TopicView({ slug }: { slug: string }) {
  const resource = useTopics()
  const [choice, setChoice] = useState<{ slug: string; view: View } | null>(null)
  if (choice && choice.slug !== slug) setChoice(null)
  const chosenView = choice?.slug === slug ? choice.view : null
  const setView = (view: View) => setChoice({ slug, view })
  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind={resource.status} what="主题" onRetry={resource.retry} />
  const topic = resource.status === 'ok' ? resource.data.topics.find(item => item.slug === slug) : undefined
  if (!topic && resource.status === 'loading') return <SkeletonCards />
  if (!topic) return <ErrorPage kind="notfound" what="主题" />
  const view = chosenView ?? defaultTopicView(topic)
  const head = <header className="page-head">
    <div>
      <p className="topic-eyebrow"><Link to="/topics">主题</Link></p>
      <h1>{topic.name}</h1>
      <p>{topic.description}</p>
      {topic.group === 'genre' && <p><a className="inline-rss" href={`/feed/category/${topic.slug}.xml`}><IconRss size={13} />订阅{topic.name}精选 RSS</a></p>}
    </div>
    {/* 侧栏里没有主题页的精选和全部入口，所以这个切换在所有宽度都显示。 */}
    <div className="view-switch is-always" role="group" aria-label="切换列表">
      <button type="button" aria-pressed={view === 'selected'} onClick={() => setView('selected')}>精选</button>
      <button type="button" aria-pressed={view === 'all'} onClick={() => setView('all')}>全部</button>
    </div>
  </header>
  return <Feed view={view} topic={slug} rss={false} head={head} />
}
