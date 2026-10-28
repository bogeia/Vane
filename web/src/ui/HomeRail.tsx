import { Link } from 'react-router'
import { useReports, useTopics } from '../lib/reading'
import { useResource } from '../lib/resource'
import { IconDoc, IconFlame, IconGrid } from './icons'
import './page.css'
import './rank.css'
import './home-rail.css'

type HotStory = { story_id: string; title: string; heat: number; sources: { name: string }[] }

// 内容右栏：热点榜前 6、最新一期日报、有精选的主题。热点榜页自己就是榜单，不再重复热点。
export function HomeRail({ hot: showHot = true, excludeTopic, report: showReport = true }: { hot?: boolean; excludeTopic?: string; report?: boolean }) {
  const hot = useResource<{ items: HotStory[] }>(showHot ? '/api/hot' : null, { progress: false })
  const reports = useReports({ progress: false })
  const topics = useTopics({ progress: false })
  const latest = reports.status === 'ok' ? reports.data.items[0] : undefined
  const topicChips = topics.status === 'ok' ? topics.data.topics.filter(topic => topic.selected_count > 0 && topic.slug !== excludeTopic).slice(0, 14) : []
  return <>
    {showHot && <section className="panel" aria-labelledby="rail-hot">
      <div className="panel-head"><h2 id="rail-hot"><IconFlame size={16} />热点榜</h2><Link to="/hot" aria-label="查看全部热点榜">全部</Link></div>
      {hot.status === 'ok' && (hot.data.items.length === 0
        ? <p className="panel-note">最近 48 小时还没有至少两家信源同时关注的事件。</p>
        : <ol className="rail-hot">{hot.data.items.slice(0, 6).map((story, index) => <li key={story.story_id}>
          <span className={`rank rank-${Math.min(index + 1, 4)}`}>{index + 1}</span>
          <div><Link to={`/stories/${story.story_id}`}>{story.title}</Link><p>{story.sources.length} 家报道 · 热度 {story.heat}</p></div>
        </li>)}</ol>)}
      {(hot.status === 'failed' || hot.status === 'notfound') && <p className="panel-note">热点榜暂时加载不出来。</p>}
    </section>}
    {showReport && latest && <section className="panel" aria-labelledby="rail-report">
      <div className="panel-head"><h2 id="rail-report"><IconDoc size={16} />日报</h2><Link to="/reports" aria-label="查看往期日报">往期</Link></div>
      <p className="rail-report-date">{latest.date}</p>
      <Link className="rail-report-headline" to={`/reports/${latest.date}`}>{latest.headline || '本期空刊'}</Link>
    </section>}
    {topicChips.length > 0 && <section className="panel" aria-labelledby="rail-topics">
      <div className="panel-head"><h2 id="rail-topics"><IconGrid size={16} />主题</h2><Link to="/topics" aria-label="查看全部主题">全部</Link></div>
      <div className="topic-chips">{topicChips.map(topic => <Link key={topic.slug} to={`/topics/${topic.slug}`}>{topic.name}<span>{topic.selected_count}</span></Link>)}</div>
    </section>}
  </>
}
