import { Link } from 'react-router'
import { useReports } from '../lib/reading'
import { reportWeekday } from '../lib/time'
import { IconRss } from '../ui/icons'
import { Empty, ErrorPage, SkeletonRows } from '../ui/States'
import '../ui/page.css'
import './reports.css'

// 日报列表 /reports：每期一行。
export function ReportsPage() {
  const resource = useReports()
  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind="failed" what="日报" onRetry={resource.retry} />
  return <>
    <header className="page-head">
      <div>
        <h1>日报</h1>
        <p>每天 08:00（北京时间）出刊，收录前一日 08:00 至当日 08:00 处理完的精选 · <a className="inline-rss" href="/feed/daily.xml"><IconRss size={13} />订阅日报 RSS</a></p>
      </div>
    </header>
    {resource.status === 'loading' && <SkeletonRows />}
    {resource.status === 'ok' && (resource.data.items.length === 0
      ? <Empty>还没有出过日报。日报每天 08:00（北京时间）出刊。</Empty>
      : <ol className="issue-list">{resource.data.items.map(issue => <li key={issue.date}>
        <Link to={`/reports/${issue.date}`}>
          <time dateTime={issue.date}>{issue.date}</time>
          <span className="issue-weekday">{reportWeekday(issue.date)}</span>
          <span className={issue.headline ? 'issue-headline' : 'issue-headline is-empty'}>{issue.headline || '本期空刊'}</span>
        </Link>
      </li>)}</ol>)}
  </>
}
