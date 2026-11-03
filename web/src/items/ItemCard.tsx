import { useState } from 'react'
import { Link } from 'react-router'
import { clock } from '../lib/time'
import { useReadingState } from '../useReadingState'
import { IconBookmark, IconExternal } from '../ui/icons'
import { Picture } from '../ui/Picture'
import type { Item } from './types'
import { useFavorite } from './useFavorite'
import './items.css'

// 信源首字头像的底色按信源名算出色相，同一个信源颜色固定。
const hue = (name: string) => [...name].reduce((sum, char) => sum + char.codePointAt(0)!, 0) % 360

export function SourceAvatar({ name, iconURL }: { name: string; iconURL: string }) {
  return <SourceAvatarImage key={iconURL} name={name} logo={iconURL} />
}

function SourceAvatarImage({ name, logo }: { name: string; logo?: string }) {
  const [failed, setFailed] = useState(false)
  const showLogo = logo && !failed
  return <span className="source-avatar" style={{ background: showLogo ? '#fff' : `hsl(${hue(name)} 45% 32%)` }} aria-hidden="true">
    {showLogo ? <img src={logo} alt="" width="20" height="20" onError={() => setFailed(true)} /> : [...name][0]}
  </span>
}

// 资料卡片。lead 是日报头条用的变体：图片通栏，摘要和推荐理由不截断。
export function ItemCard({ item, lead = false }: { item: Item; lead?: boolean }) {
  const reading = useReadingState()
  const favorite = useFavorite(item)
  return <article className={`card${lead ? ' is-lead' : ''}${reading.isRead(item.id) ? ' is-read' : ''}`} data-shortcut-item>
    <div className="card-body">
      {lead && <Picture className="card-lead-image" src={item.image_url} />}
      <h3 className="card-title"><Link to={`/items/${item.id}`} data-shortcut-title>{item.title}</Link></h3>
      <p className="card-summary">{item.summary}</p>
      {lead && item.reason && <p className="card-reason">{item.reason}</p>}
      {item.story_others > 0 && <Link className="card-others" to={`/items/${item.id}#story`}>同一事件 · 另有 {item.story_others} 家报道</Link>}
      <div className="card-actions">
        <div className="card-meta">
          <SourceAvatar name={item.source.name} iconURL={item.source.icon_url} />
          <span className="card-source">{item.source.name}</span>
          <time dateTime={item.published_at}>{clock(item.published_at)}</time>
          {item.category && <span className="category-tag">{item.category}</span>}
        </div>
        <ReadableLinks item={item} />
        <span className="score">{item.score} 分</span>
        {!lead && (item.reason || item.image_url) && <details className="card-context"><summary>{item.reason ? (item.selected ? '精选理由' : '资料评估') : '配图'}</summary>{item.reason && <p>{item.reason}</p>}<Picture className="card-context-image" src={item.image_url} /></details>}
        {favorite.message && <span className="favorite-alert" role="alert">{favorite.message}</span>}
        <button type="button" className="icon-button" aria-label="收藏" title={favorite.saved ? '取消收藏' : '收藏'} aria-pressed={favorite.saved} onClick={favorite.toggle} data-shortcut-favorite><IconBookmark size={17} /><span className="card-action-label">{favorite.saved ? '已收藏' : '收藏'}</span></button>
        <a className="icon-button" href={item.url} target="_blank" rel="noopener noreferrer" aria-label="阅读原文（新标签页）" title="阅读原文" onClick={() => reading.markRead([item.id])} data-shortcut-original><IconExternal size={17} /><span className="card-action-label">原文</span></a>
      </div>
    </div>
  </article>
}

// 可读性由列表接口提供，不为列表另取每份详情。
export function ReadableLinks({ item }: { item: Item }) {
  return <>{item.has_body && <Link className="readable-link" to={`/items/${item.id}#body`}>正文</Link>}{item.has_discussion && <Link className="readable-link" to={`/items/${item.id}#discussion`}>社区讨论</Link>}</>
}
