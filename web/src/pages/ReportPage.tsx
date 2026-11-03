import { Link, useParams } from 'react-router'
import { ItemCard, SourceAvatar } from '../items/ItemCard'
import { reportContents, reportItemAnchor, type Report } from '../lib/reportContents'
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

// 日报详情 /reports/:date。
export function ReportPage() {
  const { date = '' } = useParams()
  const resource = useResource<Report>(`/api/reports/${encodeURIComponent(date)}`)
  const list = useReports({ progress: false })
  const issues = list.status === 'ok' ? list.data.items : []
  const { older, newer } = adjacentIssues(issues.map(issue => issue.date), date)

  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind={resource.status} what="日报" onRetry={resource.retry} />
  const report = resource.status === 'ok' ? resource.data : null
  const contents = report ? reportContents(report) : null
  const count = contents?.count ?? 0
  const empty = report !== null && count === 0
  const nav = <IssueNav older={older} newer={newer} />

  const rail = <>
    {report && !empty && <TableOfContents title="本期目录">
      {report.headline && <li><a href="#r-headline">头条</a></li>}
      {report.highlights.length > 0 && <li><a href="#r-highlights">看点<span>{report.highlights.length}</span></a></li>}
      {contents!.sections.map((section, index) => <li key={index}><a href={`#r-section-${index}`}>{section.category}<span>{section.items.length}</span></a></li>)}
      {contents!.briefs.length > 0 && <li><a href="#r-briefs">快讯<span>{contents!.briefs.length}</span></a></li>}
    </TableOfContents>}
    {issues.length > 0 && <section className="panel" aria-labelledby="issues-title">
      <div className="panel-head"><h2 id="issues-title">往期</h2><Link to="/reports" aria-label="查看全部往期日报">全部</Link></div>
      <ul className="recent-issues">{issues.slice(0, 7).map(issue => <li key={issue.date}>
        <Link to={`/reports/${issue.date}`} aria-current={issue.date === date ? 'page' : undefined}><time dateTime={issue.date}>{shortDate(issue.date)}</time><span>{issue.headline || '本期空刊'}</span></Link>
      </li>)}</ul>
    </section>}
  </>

  return <Columns rail={rail}><div className="report-reading">
    <header className="page-head report-head">
      <div>
        <h1>日报 · {reportDateLabel(date)}</h1>
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
    {report && (report.headline || report.highlights.length > 0) && <nav className="report-index" aria-label="本期重点索引">
      {report.headline && <section id="r-headline"><h2>头条</h2><a className="report-headline-link" href={`#${encodeURIComponent(reportItemAnchor(report.headline.id))}`}>{report.headline.title}</a><p className="report-index-source"><SourceAvatar name={report.headline.source.name} iconURL={report.headline.source.icon_url} />{report.headline.source.name}</p></section>}
      {report.highlights.length > 0 && <section id="r-highlights"><h2>看点</h2><ul>{[...new Map(report.highlights.map(item => [item.id, item])).values()].map(item => <li key={item.id}><a href={`#${encodeURIComponent(reportItemAnchor(item.id))}`}>{item.title}</a><span><SourceAvatar name={item.source.name} iconURL={item.source.icon_url} />{item.source.name}</span></li>)}</ul></section>}
    </nav>}
    {contents?.sections.map((section, index) => <section key={index} id={`r-section-${index}`} className="report-section" aria-labelledby={`r-section-${index}-title`}>
      <h2 className="report-section-title" id={`r-section-${index}-title`}>{section.category}<span>{section.items.length} 条</span></h2>
      {section.items.map(item => <div id={reportItemAnchor(item.id)} className="report-item" key={item.id}><ItemCard item={item} /></div>)}
    </section>)}
    {!!contents?.briefs.length && <section id="r-briefs" className="report-section" aria-labelledby="r-briefs-title">
      <h2 className="report-section-title" id="r-briefs-title">快讯<span>{contents.briefs.length} 条</span></h2>
      {contents.briefs.map(item => <div id={reportItemAnchor(item.id)} className="report-item" key={item.id}><ItemCard item={item} /></div>)}
    </section>}
    {report && <div className="report-foot">{nav}</div>}
  </div></Columns>
}

function IssueNav({ older, newer }: { older?: string; newer?: string }) {
  return <nav className="issue-nav" aria-label="切换期数">
    {older ? <Link to={`/reports/${older}`} aria-label={`上一期 ${older}`}>‹ {shortDate(older)}</Link> : <span>‹ 没有更早的</span>}
    {newer ? <Link to={`/reports/${newer}`} aria-label={`下一期 ${newer}`}>{shortDate(newer)} ›</Link> : <span>已是最新 ›</span>}
  </nav>
}
