import { Link } from 'react-router'
import { useResource } from '../lib/resource'
import { Columns } from '../shell/Shell'
import { HomeRail } from '../ui/HomeRail'
import { Empty, ErrorPage, SkeletonRows } from '../ui/States'
import '../ui/page.css'
import '../ui/rank.css'
import './reports.css'

type HotStory = { story_id: string; title: string; heat: number; trend: string; sources: { id: string; name: string }[] }
const TRENDS: Record<string, string> = { new: '新', rising: '上升' }

// 热点榜 /hot。
export function HotPage() {
  const resource = useResource<{ items: HotStory[] }>('/api/hot')
  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind="failed" what="热点榜" onRetry={resource.retry} />
  return <Columns rail={<HomeRail hot={false} />}>
    <header className="page-head">
      <div>
        <h1>热点榜</h1>
        <p>最近 48 小时至少两家信源共同关注的事件。每个信源只计最新资料，热度每 24 小时减半；趋势与 6 小时前相比，数据缓存 5 分钟。</p>
      </div>
    </header>
    {resource.status === 'loading' && <SkeletonRows />}
    {resource.status === 'ok' && (resource.data.items.length === 0
      ? <Empty>最近 48 小时还没有至少两家信源同时关注的事件。</Empty>
      : <ol className="hot-board">{resource.data.items.map((story, index) => <li key={story.story_id} data-shortcut-item>
        <span className={`rank rank-${Math.min(index + 1, 4)}`}><span className="visually-hidden">第 </span>{index + 1}<span className="visually-hidden"> 名</span></span>
        <div className="hot-body">
          <Link className="hot-title" to={`/stories/${story.story_id}`} data-shortcut-title>{story.title}</Link>
          <p className="hot-sources">{story.sources.map(source => source.name).join('、')}</p>
        </div>
        <div className="hot-stats">
          <span className="hot-heat">热度 {story.heat}</span>
          {TRENDS[story.trend] && <span className="hot-trend">{TRENDS[story.trend]}</span>}
        </div>
      </li>)}</ol>)}
  </Columns>
}
