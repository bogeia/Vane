import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { latestRelease, useChangelogUnseen } from '../changelogState'
import { NewDot } from '../shell/Shell'
import { setTheme, THEMES, useTheme } from '../shell/theme'
import { useReadingState } from '../useReadingState'
import { IconBookmark, IconChevron, IconGrid, IconHistory, IconInfo, IconRss, type Icon } from '../ui/icons'
import './more.css'

// 我的 /more：手机底部“我的”标签进入，桌面也能打开。
export function MorePage() {
  const favorites = useReadingState().favorites().length
  const theme = useTheme()
  const unseen = useChangelogUnseen()
  return <div className="more">
    <h1>我的</h1>
    <section aria-labelledby="more-reading">
      <h2 id="more-reading">阅读</h2>
      <div className="more-group">
        <MoreRow to="/favorites" icon={IconBookmark} label="收藏" note={favorites > 0 ? `${favorites} 条，存在这个浏览器里` : '还没有收藏'} />
        <MoreRow to="/topics" icon={IconGrid} label="主题" note="按公司、方向和内容形态看资料" />
      </div>
    </section>
    <section aria-labelledby="more-theme">
      <h2 id="more-theme">外观</h2>
      <div className="more-group theme-options" role="radiogroup" aria-labelledby="more-theme">
        {THEMES.map(option => <label key={option.value}>
          <input type="radio" name="theme" value={option.value} checked={theme === option.value} onChange={() => setTheme(option.value)} />
          {option.label}
        </label>)}
      </div>
    </section>
    <section aria-labelledby="more-about">
      <h2 id="more-about">关于</h2>
      <div className="more-group">
        <MoreRow to="/about" icon={IconInfo} label="关于 Vane" note="它做什么、怎么挑资料、有哪些信源" />
        <MoreRow to="/agent" icon={IconRss} label="接入 Agent" note="让 AI 助手直接读精选、热点和日报" />
        <MoreRow to="/changelog" icon={IconHistory} label="更新日志" note={`${latestRelease.title} · ${latestRelease.date}`} dot={unseen} />
      </div>
    </section>
    <section aria-labelledby="more-feeds">
      <h2 id="more-feeds">订阅</h2>
      <div className="more-group">
        <MoreRow href="/feed.xml" icon={IconRss} label="精选 RSS" note="/feed.xml" />
        <MoreRow href="/feed/daily.xml" icon={IconRss} label="日报 RSS" note="/feed/daily.xml" />
        <MoreRow href="/sources.opml" icon={IconRss} label="信源列表 OPML" note="/sources.opml，可以导入你的阅读器" />
      </div>
    </section>
  </div>
}

function MoreRow({ to, href, icon: RowIcon, label, note, dot = false }: { to?: string; href?: string; icon: Icon; label: string; note: string; dot?: boolean }) {
  const content: ReactNode = <>
    <span className="more-icon"><RowIcon size={19} /></span>
    <span className="more-text"><b>{label}</b><small>{note}</small></span>
    {dot && <NewDot />}
    <IconChevron size={16} />
  </>
  return to ? <Link className="more-row" to={to}>{content}</Link> : <a className="more-row" href={href}>{content}</a>
}
