import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { ItemCard } from '../items/ItemCard'
import { ItemRow } from '../items/ItemRow'
import type { Item } from '../items/types'
import { adjacentIssues } from '../lib/issues'
import { useReports } from '../lib/reading'
import { useResource } from '../lib/resource'
import { reportDateLabel, shortDate } from '../lib/time'
import { Columns } from '../shell/Shell'
import { TableOfContents } from '../ui/TableOfContents'
import { IconRss } from '../ui/icons'
import { Empty, ErrorPage, SkeletonCards } from '../ui/States'
import '../ui/page.css'
import './reports.css'

type Report = { date: string; headline: Item | null; highlights: Item[]; sections: { category: string; items: Item[] }[]; briefs: Item[] }

// 日报详情 /reports/:date。
export function ReportPage() {
  const { date = '' } = useParams()
  const resource = useResource<Report>(`/api/reports/${encodeURIComponent(date)}`)
  const list = useReports({ progress: false })
  const issues = list.status === 'ok' ? list.data.items : []
  const { older, newer } = adjacentIssues(issues.map(issue => issue.date), date)
  const [openBrief, setOpenBrief] = useState('')

  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind={resource.status} what="日报" onRetry={resource.retry} />
  const report = resource.status === 'ok' ? resource.data : null
  // 正文按分类分节时已经包含头条和看点，所以条数是各节加快讯。
  const count = report ? report.sections.reduce((sum, section) => sum + section.items.length, 0) + report.briefs.length : 0
  const empty = report !== null && !report.headline && report.sections.length === 0 && report.briefs.length === 0
  const nav = <IssueNav older={older} newer={newer} />

  const rail = <>
    {report && !empty && <TableOfContents title="本期目录">
      {report.headline && <li><a href="#r-headline">头条</a></li>}
      {report.highlights.length > 0 && <li><a href="#r-highlights">看点<span>{report.highlights.length}</span></a></li>}
      {report.sections.map((section, index) => <li key={section.category}><a href={`#r-section-${index}`}>{section.category}<span>{section.items.length}</span></a></li>)}
      {report.briefs.length > 0 && <li><a href="#r-briefs">快讯<span>{report.briefs.length}</span></a></li>}
    </TableOfContents>}
    {issues.length > 0 && <section className="panel" aria-labelledby="issues-title">
      <div className="panel-head"><h2 id="issues-title">往期</h2><Link to="/reports" aria-label="查看全部往期日报">全部</Link></div>
      <ul className="recent-issues">{issues.slice(0, 7).map(issue => <li key={issue.date}>
        <Link to={`/reports/${issue.date}`} aria-current={issue.date === date ? 'page' : undefined}><time dateTime={issue.date}>{shortDate(issue.date)}</time><span>{issue.headline || '本期空刊'}</span></Link>
      </li>)}</ul>
    </section>}
  </>

  return <Columns rail={rail}>
    <header className="page-head report-head">
      <div>
        <p className="report-eyebrow">Vane 日报</p>
        <h1>{reportDateLabel(date)}</h1>
        <p className="report-facts">
          <span>覆盖前一日 08:00 至当日 08:00（北京时间）</span>
          {report && <span>共 {count} 条</span>}
          <a href="/feed/daily.xml"><IconRss size={13} />订阅</a>
          <Link to="/reports">往期</Link>
        </p>
      </div>
      {nav}
    </header>
    {!report && <SkeletonCards />}
    {empty && <Empty>这一期没有达到收录标准的资料。可以看看<Link to="/reports">往期</Link>，或者回到<Link to="/">精选</Link>。</Empty>}
    {report?.headline && <section id="r-headline" className="report-section" aria-labelledby="r-headline-title">
      <h2 className="report-section-title" id="r-headline-title">头条</h2>
      <ItemCard item={report.headline} lead />
    </section>}
    {report && report.highlights.length > 0 && <section id="r-highlights" className="report-section" aria-labelledby="r-highlights-title">
      <h2 className="report-section-title" id="r-highlights-title">看点</h2>
      <ol className="highlights">{report.highlights.map((item, index) => <li key={item.id}>
        <span aria-hidden="true">{index + 1}</span>
        <div><Link to={`/items/${item.id}`}>{item.title}</Link><p>{item.source.name} · {item.score} 分</p></div>
      </li>)}</ol>
    </section>}
    {report?.sections.map((section, index) => <section key={section.category} id={`r-section-${index}`} className="report-section" aria-labelledby={`r-section-${index}-title`}>
      <h2 className="report-section-title" id={`r-section-${index}-title`}>{section.category}<span>{section.items.length} 条</span></h2>
      {section.items.map(item => <ItemCard key={item.id} item={item} />)}
    </section>)}
    {report && report.briefs.length > 0 && <section id="r-briefs" className="report-section" aria-labelledby="r-briefs-title">
      <h2 className="report-section-title" id="r-briefs-title">快讯<span>{report.briefs.length} 条</span></h2>
      <ol className="rows">{report.briefs.map(item => <ItemRow key={item.id} item={item} open={openBrief === item.id} onToggle={() => setOpenBrief(openBrief === item.id ? '' : item.id)} />)}</ol>
    </section>}
    {report && <div className="report-foot">{nav}</div>}
  </Columns>
}

function IssueNav({ older, newer }: { older?: string; newer?: string }) {
  return <nav className="issue-nav" aria-label="切换期数">
    {older ? <Link to={`/reports/${older}`} aria-label={`上一期 ${older}`}>‹ {shortDate(older)}</Link> : <span>‹ 没有更早的</span>}
    {newer ? <Link to={`/reports/${newer}`} aria-label={`下一期 ${newer}`}>{shortDate(newer)} ›</Link> : <span>已是最新 ›</span>}
  </nav>
}
