import { useEffect } from 'react'
import { changelogSeen, releases } from '../changelogState'
import { reportWeekday } from '../lib/time'
import { Columns } from '../shell/Shell'
import { TableOfContents } from '../ui/TableOfContents'
import '../ui/page.css'
import './more.css'

// 更新日志 /changelog：每个版本一个区块。打开后记下看过的最新一条，红点同时消失。
export function ChangelogPage() {
  useEffect(() => changelogSeen.markSeen(), [])
  const rail = <TableOfContents>
    {releases.map(release => <li key={release.id}><a href={`#release-${release.id}`}>{release.title}</a></li>)}
  </TableOfContents>
  return <Columns rail={rail}><div className="prose-page">
    <header className="page-head"><div><h1>更新日志</h1><p>每个版本改了什么，按日期从新到旧。</p></div></header>
    {releases.map(release => <section className="release" key={release.id} aria-labelledby={`release-${release.id}`}>
      <p className="release-date"><time dateTime={release.date}>{release.date}</time> · {reportWeekday(release.date)}</p>
      <h2 id={`release-${release.id}`}>{release.title}</h2>
      <ul>{release.items.map(item => <li key={item}>{item}</li>)}</ul>
    </section>)}
  </div></Columns>
}
